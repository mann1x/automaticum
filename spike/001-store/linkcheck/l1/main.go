// Command linkcheck-l1 is threshold T1 of the store spike for Lance candidate
// L1 (lancedb-go): it opens DuckDB and a Lance table in one process, writes
// to both and reads back, and prints one JSON object with what it saw.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

const dim = 64

type result struct {
	GOOS, GOARCH  string
	DuckDBVersion string
	DuckRows      int
	DuckReopened  bool
	LanceRows     int64
	FTSHits       int64
	VectorHits    int64
	HybridHits    int64
	Elapsed       string
}

func main() {
	dir, err := os.MkdirTemp("", "automaticum-linkcheck-")
	check(err)
	defer os.RemoveAll(dir)
	start := time.Now()
	res := result{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}

	duck(filepath.Join(dir, "state.duckdb"), &res)
	lance(filepath.Join(dir, "lance"), &res)
	// DuckDB again after Lance has run in the same process.
	db := openDuck(filepath.Join(dir, "state.duckdb"))
	check(db.QueryRow(`SELECT count(*) FROM runs`).Scan(&res.DuckRows))
	check(db.Close())
	res.DuckReopened = true

	res.Elapsed = time.Since(start).Round(time.Millisecond).String()
	out, _ := json.Marshal(res)
	fmt.Println(string(out))
	if res.DuckRows != 1000 || res.LanceRows != 200 || res.FTSHits == 0 || res.VectorHits == 0 || res.HybridHits == 0 {
		fmt.Fprintln(os.Stderr, "linkcheck: FAIL")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "linkcheck: PASS")
}

func openDuck(path string) *sql.DB {
	db, err := sql.Open("duckdb", path)
	check(err)
	// The store needs no downloadable extension; make that impossible.
	_, err = db.Exec(`SET autoinstall_known_extensions = false; SET autoload_known_extensions = false`)
	check(err)
	return db
}

func duck(path string, res *result) {
	db := openDuck(path)
	check(db.QueryRow(`SELECT version()`).Scan(&res.DuckDBVersion))
	_, err := db.Exec(`CREATE TABLE runs (id VARCHAR PRIMARY KEY, verdict VARCHAR, wall_ms BIGINT)`)
	check(err)
	_, err = db.Exec(`INSERT INTO runs SELECT 'run-' || i, CASE WHEN i % 3 = 0 THEN 'FAIL' ELSE 'PASS' END, i * 7 FROM range(1000) t(i)`)
	check(err)
	var p50 float64
	check(db.QueryRow(`SELECT quantile_cont(wall_ms, 0.5) FROM runs WHERE verdict = 'PASS'`).Scan(&p50))
	check(db.Close())
}

func lance(path string, res *result) {
	ctx := context.Background()
	conn, err := lancedb.Connect(ctx, path, nil)
	check(err)
	defer conn.Close()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		{Name: "body", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "embedding", Type: arrow.FixedSizeListOf(dim, arrow.PrimitiveTypes.Float32), Nullable: false},
	}, nil)
	schema, err := lancedb.NewSchema(arrowSchema)
	check(err)
	table, err := conn.CreateTable(ctx, "chunks", schema)
	check(err)
	defer table.Close()

	docs := []string{
		"the model decided the oracle was wrong and edited the test",
		"reasoning repeats the same sentence about the countdown",
		"tool call could not be read and was repaired",
		"compaction ran before the context window was full",
	}
	const n = 200
	pool := memory.NewGoAllocator()
	idB := array.NewInt32Builder(pool)
	bodyB := array.NewStringBuilder(pool)
	embB := array.NewFixedSizeListBuilder(pool, dim, arrow.PrimitiveTypes.Float32)
	valB := embB.ValueBuilder().(*array.Float32Builder)
	for i := 0; i < n; i++ {
		idB.Append(int32(i))
		bodyB.Append(docs[i%len(docs)])
		embB.Append(true)
		for j := 0; j < dim; j++ {
			valB.Append(float32(i)*0.01 + float32(j)*0.001)
		}
	}
	rec := array.NewRecord(arrowSchema, []arrow.Array{idB.NewArray(), bodyB.NewArray(), embB.NewArray()}, n)
	defer rec.Release()
	check(table.Add(ctx, rec, nil))

	res.LanceRows, err = table.Count(ctx)
	check(err)

	check(table.CreateIndexWithParams(ctx, []string{"body"}, contracts.IndexTypeFts, contracts.IndexParams{},
		&contracts.CreateIndexOptions{Name: "body_fts", WaitTimeout: 60 * time.Second}))

	query := make([]float32, dim)
	for j := range query {
		query[j] = 0.5 + float32(j)*0.001
	}
	vec, err := table.VectorQuery("embedding", query).Limit(5).Execute(ctx)
	check(err)
	res.VectorHits = vec.NumRows()
	vec.Release()

	hyb, err := table.VectorQuery("embedding", query).WithFullText("oracle wrong", "body").
		Rerank(contracts.RerankerConfig{Kind: contracts.RerankerRRF, RRFK: 60, Norm: contracts.NormalizeRank}).
		Limit(5).Execute(ctx)
	check(err)
	res.HybridHits = hyb.NumRows()
	hyb.Release()

	fts, err := table.VectorQuery("embedding", query).WithFullText("countdown", "body").Limit(5).Execute(ctx)
	check(err)
	res.FTSHits = fts.NumRows()
	fts.Release()
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "linkcheck:", err)
		os.Exit(1)
	}
}
