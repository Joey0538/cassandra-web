package main

import "testing"

func col(name, kind, typ string) map[string]interface{} {
	return map[string]interface{}{"column_name": name, "kind": kind, "type": typ}
}

// A row's identity is its partition key plus whatever clustering keys it has.
// The static row of a partition has none, and CQL rejects a null in a WHERE
// clause, so those columns must be left out rather than bound as null.
func TestDeleteWhere(t *testing.T) {
	schema := []map[string]interface{}{
		col("pk", PartitionKey, "text"),
		col("ck1", ClusteringKey, "int"),
		col("ck2", ClusteringKey, "int"),
		col("v", "regular", "text"),
	}

	cases := []struct {
		name       string
		item       map[string]interface{}
		wantCql    string
		wantValues int
	}{
		{
			name:       "full primary key",
			item:       map[string]interface{}{"pk": "p", "ck1": int64(1), "ck2": int64(2), "v": "x"},
			wantCql:    "pk = ? AND ck1 = ? AND ck2 = ?",
			wantValues: 3,
		},
		{
			// The row left behind once a partition's last clustering row is
			// deleted; deleting it has to remove the whole partition.
			name:       "static row has no clustering key",
			item:       map[string]interface{}{"pk": "p", "ck1": nil, "ck2": nil, "v": nil},
			wantCql:    "pk = ?",
			wantValues: 1,
		},
		{
			// CQL only accepts a contiguous clustering prefix.
			name:       "stops at the first missing clustering key",
			item:       map[string]interface{}{"pk": "p", "ck1": int64(1), "ck2": nil},
			wantCql:    "pk = ? AND ck1 = ?",
			wantValues: 2,
		},
		{
			name:       "absent clustering key is the same as null",
			item:       map[string]interface{}{"pk": "p"},
			wantCql:    "pk = ?",
			wantValues: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cql, values, err := deleteWhere(converter{}, schema, tc.item)
			if err != nil {
				t.Fatalf("deleteWhere: %v", err)
			}
			if cql != tc.wantCql {
				t.Fatalf("cql = %q, want %q", cql, tc.wantCql)
			}
			if len(values) != tc.wantValues {
				t.Fatalf("values = %d, want %d", len(values), tc.wantValues)
			}
		})
	}
}

// Without a partition key the statement would delete across the whole table,
// so it must be refused instead of built.
func TestDeleteWhere_RequiresPartitionKey(t *testing.T) {
	schema := []map[string]interface{}{
		col("pk", PartitionKey, "text"),
		col("v", "regular", "text"),
	}

	if _, _, err := deleteWhere(converter{}, schema, map[string]interface{}{"v": "x"}); err == nil {
		t.Fatal("expected an error when the partition key is missing")
	}
}
