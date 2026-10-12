// Command linkcheck-l2 is threshold T1 of the store spike for Lance candidate
// L2: the official C interface (lance-c) behind a thin cgo wrapper of our own,
// exchanging data through the Arrow C Data Interface. It opens DuckDB and a
// Lance dataset in one process, writes to both and reads back.
//
// Build with CGO_CFLAGS=-I<lance-c>/include and CGO_LDFLAGS naming
// liblance_c.a (see ../README.md).
package main

/*
#include <stdlib.h>
#include <lance/lance.h>

static struct ArrowSchema*      new_schema(void) { return calloc(1, sizeof(struct ArrowSchema)); }
static struct ArrowArrayStream* new_stream(void) { return calloc(1, sizeof(struct ArrowArrayStream)); }
*/
import "C"

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
	"github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/duckdb/duckdb-go/v2"
)

const dim = 64

type result struct {
	GOOS, GOARCH  string
	DuckDBVersion string
	DuckRows      int
	DuckReopened  bool
	LanceRows     uint64
	FTSHits       int64
	VectorHits    int64
	Elapsed       string
}

func main() {
	dir, err := os.MkdirTemp("", "automaticum-linkcheck-")
	check(err)
	defer os.RemoveAll(dir)
	start := time.Now()
	res := result{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}

	duck(filepath.Join(dir, "state.duckdb"), &res)
	lance(filepath.Join(dir, "chunks.lance"), &res)
	// DuckDB again after Lance has run in the same process.
	db := openDuck(filepath.Join(dir, "state.duckdb"))
	check(db.QueryRow(`SELECT count(*) FROM runs`).Scan(&res.DuckRows))
	check(db.Close())
	res.DuckReopened = true

	res.Elapsed = time.Since(start).Round(time.Millisecond).String()
	out, _ := json.Marshal(res)
	fmt.Println(string(out))
	if res.DuckRows != 1000 || res.LanceRows != 200 || res.FTSHits == 0 || res.VectorHits == 0 {
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

func lanceErr(what string) error {
	msg := C.lance_last_error_message()
	if msg == nil {
		return fmt.Errorf("%s: failed with no message", what)
	}
	defer C.lance_free_string(msg)
	return fmt.Errorf("%s: %s", what, C.GoString(msg))
}

func lance(path string, res *result) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		{Name: "body", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "embedding", Type: arrow.FixedSizeListOf(dim, arrow.PrimitiveTypes.Float32), Nullable: false},
	}, nil)
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
	rec := array.NewRecordBatch(schema, []arrow.Array{idB.NewArray(), bodyB.NewArray(), embB.NewArray()}, n)
	defer rec.Release()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
	check(err)
	defer reader.Release()

	cSchema := C.new_schema()
	cStream := C.new_stream()
	defer C.free(unsafe.Pointer(cSchema))
	defer C.free(unsafe.Pointer(cStream))
	cdata.ExportArrowSchema(schema, (*cdata.CArrowSchema)(unsafe.Pointer(cSchema)))
	cdata.ExportRecordReader(reader, (*cdata.CArrowArrayStream)(unsafe.Pointer(cStream)))

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var ds *C.LanceDataset
	if C.lance_dataset_write(cPath, cSchema, cStream, C.LANCE_WRITE_CREATE, nil, &ds) != 0 {
		check(lanceErr("write"))
	}
	defer func() { C.lance_dataset_close(ds) }()
	res.LanceRows = uint64(C.lance_dataset_count_rows(ds))

	cBody := C.CString("body")
	cName := C.CString("body_fts")
	defer C.free(unsafe.Pointer(cBody))
	defer C.free(unsafe.Pointer(cName))
	if C.lance_dataset_create_scalar_index(ds, cBody, cName, C.LANCE_SCALAR_INVERTED, nil, C.bool(true)) != 0 {
		check(lanceErr("create fts index"))
	}

	// Full-text query.
	sc := C.lance_scanner_new(ds, nil, nil)
	if sc == nil {
		check(lanceErr("scanner"))
	}
	cQuery := C.CString("countdown")
	defer C.free(unsafe.Pointer(cQuery))
	if C.lance_scanner_full_text_search(sc, cQuery, nil, 0) != 0 {
		check(lanceErr("fts"))
	}
	C.lance_scanner_set_limit(sc, 5)
	res.FTSHits = drain(sc)
	C.lance_scanner_close(sc)

	// Vector query (exact; no index on 200 rows).
	sc = C.lance_scanner_new(ds, nil, nil)
	if sc == nil {
		check(lanceErr("scanner"))
	}
	query := make([]float32, dim)
	for j := range query {
		query[j] = 0.5 + float32(j)*0.001
	}
	cEmb := C.CString("embedding")
	defer C.free(unsafe.Pointer(cEmb))
	if C.lance_scanner_nearest(sc, cEmb, unsafe.Pointer(&query[0]), C.size_t(dim), C.LANCE_DTYPE_FLOAT32, 5) != 0 {
		check(lanceErr("nearest"))
	}
	res.VectorHits = drain(sc)
	C.lance_scanner_close(sc)
}

// drain reads a scanner to the end through the Arrow C stream and counts rows.
func drain(sc *C.LanceScanner) int64 {
	out := C.new_stream()
	defer C.free(unsafe.Pointer(out))
	if C.lance_scanner_to_arrow_stream(sc, out) != 0 {
		check(lanceErr("to_arrow_stream"))
	}
	rdr, err := cdata.ImportCRecordReader((*cdata.CArrowArrayStream)(unsafe.Pointer(out)), nil)
	check(err)
	if r, ok := rdr.(interface{ Release() }); ok {
		defer r.Release()
	}
	var rows int64
	for {
		rec, err := rdr.Read()
		if errors.Is(err, io.EOF) {
			return rows
		}
		check(err)
		rows += rec.NumRows()
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "linkcheck:", err)
		os.Exit(1)
	}
}
