//go:build lance

// Package lance implements store.Search on the Lance format, through the
// official C interface (lance-c) and the Arrow C Data Interface.
//
// It is built only with the "lance" build tag, because it links a large
// native library: set CGO_CFLAGS to lance-c's include directory and
// CGO_LDFLAGS to liblance_c.a. Builds without the tag (agent mode and the CLI
// on Windows) have no search engine.
//
// lance-c reports errors through thread-local storage, so every call that can
// fail runs with the goroutine locked to its OS thread until the error has
// been read.
package lance

/*
#cgo LDFLAGS: -lm -ldl -lpthread
#include <stdlib.h>
#include <lance/lance.h>

static struct ArrowSchema*      amc_new_schema(void) { return calloc(1, sizeof(struct ArrowSchema)); }
static struct ArrowArrayStream* amc_new_stream(void) { return calloc(1, sizeof(struct ArrowArrayStream)); }
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// rrfK is the constant of reciprocal rank fusion.
const rrfK = 60

// Options configures Open.
type Options struct {
	// Dir is the directory holding the dataset.
	Dir string
	// Dim is the vector dimension. It is fixed for the life of the dataset.
	Dim int
	// Metric is "cosine" (default), "l2" or "dot".
	Metric string
}

// Search is a store.Search backed by one Lance dataset.
type Search struct {
	mu     sync.RWMutex // writers exclusive; scanners share
	ds     *C.LanceDataset
	uri    string
	dim    int
	metric C.LanceMetricType
	schema *arrow.Schema
}

var _ store.Search = (*Search)(nil)

// hitColumns are read back for every hit; the vector is not.
var hitColumns = []string{"id", "run_id", "turn", "kind", "text", "blob", "offset", "embedder"}

// Open opens the dataset in o.Dir, or prepares to create it on first write.
func Open(o Options) (*Search, error) {
	if o.Dim <= 0 {
		return nil, errors.New("lance: vector dimension must be positive")
	}
	s := &Search{uri: o.Dir + "/chunks.lance", dim: o.Dim}
	switch o.Metric {
	case "", "cosine":
		s.metric = C.LANCE_METRIC_COSINE
	case "l2":
		s.metric = C.LANCE_METRIC_L2
	case "dot":
		s.metric = C.LANCE_METRIC_DOT
	default:
		return nil, fmt.Errorf("lance: unknown metric %q", o.Metric)
	}
	s.schema = arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String},
		{Name: "run_id", Type: arrow.BinaryTypes.String},
		{Name: "turn", Type: arrow.PrimitiveTypes.Int32},
		{Name: "kind", Type: arrow.BinaryTypes.String},
		{Name: "text", Type: arrow.BinaryTypes.LargeString},
		{Name: "blob", Type: arrow.BinaryTypes.String},
		{Name: "offset", Type: arrow.PrimitiveTypes.Int64},
		{Name: "embedder", Type: arrow.BinaryTypes.String},
		{Name: "vector", Type: arrow.FixedSizeListOf(int32(o.Dim), arrow.PrimitiveTypes.Float32), Nullable: true},
	}, nil)
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(s.uri); err == nil {
		cURI := C.CString(s.uri)
		defer C.free(unsafe.Pointer(cURI))
		err := call("open", func() bool {
			s.ds = C.lance_dataset_open(cURI, nil, 0)
			return s.ds != nil
		})
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// call runs f on a locked OS thread and, if f reports failure, returns
// lance-c's thread-local error.
func call(what string, f func() bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if f() {
		return nil
	}
	msg := C.lance_last_error_message()
	code := int(C.lance_last_error_code())
	if msg == nil {
		return fmt.Errorf("lance: %s: failed (code %d)", what, code)
	}
	defer C.lance_free_string(msg)
	return fmt.Errorf("lance: %s: %s (code %d)", what, C.GoString(msg), code)
}

func ok(rc C.int32_t) bool { return rc == 0 }

// Close closes the dataset.
func (s *Search) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds != nil {
		C.lance_dataset_close(s.ds)
		s.ds = nil
	}
	return nil
}

// record builds one Arrow record batch from chunks.
func (s *Search) record(chunks []store.Chunk) (arrow.RecordBatch, error) {
	pool := memory.DefaultAllocator
	id := array.NewStringBuilder(pool)
	run := array.NewStringBuilder(pool)
	turn := array.NewInt32Builder(pool)
	kind := array.NewStringBuilder(pool)
	text := array.NewLargeStringBuilder(pool)
	emb := array.NewStringBuilder(pool)
	src := array.NewStringBuilder(pool)
	off := array.NewInt64Builder(pool)
	vec := array.NewFixedSizeListBuilder(pool, int32(s.dim), arrow.PrimitiveTypes.Float32)
	val := vec.ValueBuilder().(*array.Float32Builder)
	defer func() {
		for _, b := range []array.Builder{id, run, turn, kind, text, src, off, emb, vec} {
			b.Release()
		}
	}()
	for i := range chunks {
		c := &chunks[i]
		if c.ID == "" {
			return nil, errors.New("lance: chunk has no id")
		}
		id.Append(c.ID)
		run.Append(c.RunID)
		turn.Append(int32(c.Turn))
		kind.Append(c.Kind)
		text.Append(c.Text)
		src.Append(string(c.Blob))
		off.Append(c.Offset)
		emb.Append(c.Embedder)
		switch len(c.Vector) {
		case 0:
			vec.AppendNull() // the builder fills the child slots itself
		case s.dim:
			vec.Append(true)
			val.AppendValues(c.Vector, nil)
		default:
			return nil, fmt.Errorf("lance: chunk %s has a vector of %d dimensions, dataset has %d", c.ID, len(c.Vector), s.dim)
		}
	}
	cols := []arrow.Array{id.NewArray(), run.NewArray(), turn.NewArray(), kind.NewArray(), text.NewArray(), src.NewArray(), off.NewArray(), emb.NewArray(), vec.NewArray()}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	return array.NewRecordBatch(s.schema, cols, int64(len(chunks))), nil
}

// export hands chunks to C as an Arrow stream. The caller frees the returned
// structs after lance-c has consumed the stream.
func (s *Search) export(chunks []store.Chunk) (*C.struct_ArrowSchema, *C.struct_ArrowArrayStream, func(), error) {
	rec, err := s.record(chunks)
	if err != nil {
		return nil, nil, nil, err
	}
	reader, err := array.NewRecordReader(s.schema, []arrow.RecordBatch{rec})
	rec.Release()
	if err != nil {
		return nil, nil, nil, err
	}
	cSchema, cStream := C.amc_new_schema(), C.amc_new_stream()
	cdata.ExportArrowSchema(s.schema, (*cdata.CArrowSchema)(unsafe.Pointer(cSchema)))
	cdata.ExportRecordReader(reader, (*cdata.CArrowArrayStream)(unsafe.Pointer(cStream)))
	reader.Release()
	free := func() {
		cdata.ReleaseCArrowSchema((*cdata.CArrowSchema)(unsafe.Pointer(cSchema)))
		C.free(unsafe.Pointer(cSchema))
		C.free(unsafe.Pointer(cStream))
	}
	return cSchema, cStream, free, nil
}

// write creates the dataset or appends to it, without looking for existing
// ids. The caller holds the write lock.
func (s *Search) write(chunks []store.Chunk) error {
	cSchema, cStream, free, err := s.export(chunks)
	if err != nil {
		return err
	}
	defer free()
	cURI := C.CString(s.uri)
	defer C.free(unsafe.Pointer(cURI))
	mode := C.int32_t(C.LANCE_WRITE_APPEND)
	if s.ds == nil {
		mode = C.LANCE_WRITE_CREATE
	}
	var out *C.LanceDataset
	if err := call("write", func() bool { return ok(C.lance_dataset_write(cURI, cSchema, cStream, mode, nil, &out)) }); err != nil {
		return err
	}
	if s.ds != nil {
		C.lance_dataset_close(s.ds)
	}
	s.ds = out
	return nil
}

// Append adds chunks without checking for existing ids. It is the fast path
// for a first load or a rebuild from blobs; use UpsertChunks otherwise.
func (s *Search) Append(ctx context.Context, chunks []store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(chunks)
}

// UpsertChunks inserts chunks, replacing any with the same id.
func (s *Search) UpsertChunks(ctx context.Context, chunks []store.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		return s.write(chunks)
	}
	_, cStream, free, err := s.export(chunks)
	if err != nil {
		return err
	}
	defer free()
	on := C.CString("id")
	defer C.free(unsafe.Pointer(on))
	params := C.LanceMergeInsertParams{
		when_matched:               C.LANCE_MERGE_WHEN_MATCHED_UPDATE_ALL,
		when_not_matched:           C.LANCE_MERGE_WHEN_NOT_MATCHED_INSERT_ALL,
		when_not_matched_by_source: C.LANCE_MERGE_WHEN_NOT_MATCHED_BY_SOURCE_KEEP,
	}
	return call("merge insert", func() bool {
		return ok(C.lance_dataset_merge_insert(s.ds, &on, 1, cStream, &params, nil))
	})
}

// DeleteRun removes every chunk of a run.
func (s *Search) DeleteRun(ctx context.Context, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		return nil
	}
	pred := C.CString("run_id = " + quote(runID))
	defer C.free(unsafe.Pointer(pred))
	return call("delete", func() bool { return ok(C.lance_dataset_delete(s.ds, pred, nil)) })
}

// Count returns the number of chunks.
func (s *Search) Count(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ds == nil {
		return 0, nil
	}
	return int64(C.lance_dataset_count_rows(s.ds)), nil
}

// BuildIndexes builds, or rebuilds, the full-text and vector indexes.
func (s *Search) BuildIndexes(ctx context.Context, o store.IndexOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		return nil
	}
	if o.FullText {
		col, name := C.CString("text"), C.CString("text_fts")
		defer C.free(unsafe.Pointer(col))
		defer C.free(unsafe.Pointer(name))
		err := call("full-text index", func() bool {
			return ok(C.lance_dataset_create_scalar_index(s.ds, col, name, C.LANCE_SCALAR_INVERTED, nil, C.bool(true)))
		})
		if err != nil {
			return err
		}
		// Filters on run_id are common; a scalar index keeps them cheap.
		rcol, rname := C.CString("run_id"), C.CString("run_id_btree")
		defer C.free(unsafe.Pointer(rcol))
		defer C.free(unsafe.Pointer(rname))
		err = call("run_id index", func() bool {
			return ok(C.lance_dataset_create_scalar_index(s.ds, rcol, rname, C.LANCE_SCALAR_BTREE, nil, C.bool(true)))
		})
		if err != nil {
			return err
		}
	}
	if o.Vector {
		rows := int(C.lance_dataset_count_rows(s.ds))
		parts := o.Partitions
		if parts <= 0 {
			parts = int(math.Sqrt(float64(rows)))
			if parts < 1 {
				parts = 1
			}
		}
		sub := o.SubVectors
		if sub <= 0 {
			sub = s.dim / 16
			if sub < 1 {
				sub = 1
			}
		}
		params := C.LanceVectorIndexParams{
			index_type:      C.LANCE_INDEX_IVF_PQ,
			metric:          s.metric,
			num_partitions:  C.uint32_t(parts),
			num_sub_vectors: C.uint32_t(sub),
			max_iterations:  C.uint32_t(o.MaxIterations),
			hnsw_m:          20,
		}
		col, name := C.CString("vector"), C.CString("vector_idx")
		defer C.free(unsafe.Pointer(col))
		defer C.free(unsafe.Pointer(name))
		err := call("vector index", func() bool {
			return ok(C.lance_dataset_create_vector_index(s.ds, col, name, &params, C.bool(true)))
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Optimize compacts small fragments. lance-c has no call to fold new rows
// into an existing index yet; rows written after BuildIndexes are still found
// (they are scanned without the index), and BuildIndexes rebuilds.
func (s *Search) Optimize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		return nil
	}
	return call("compact", func() bool { return ok(C.lance_dataset_compact_files(s.ds, nil, nil)) })
}

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

func in(col string, vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = quote(v)
	}
	return col + " IN (" + strings.Join(q, ", ") + ")"
}

func filter(o store.SearchOptions) string {
	var conds []string
	if len(o.RunIDs) > 0 {
		conds = append(conds, in("run_id", o.RunIDs))
	}
	if len(o.Kinds) > 0 {
		conds = append(conds, in("kind", o.Kinds))
	}
	return strings.Join(conds, " AND ")
}

// cStrings builds a NULL-terminated C array of C strings.
func cStrings(vals []string) (**C.char, func()) {
	n := len(vals) + 1
	arr := (**C.char)(C.calloc(C.size_t(n), C.size_t(unsafe.Sizeof((*C.char)(nil)))))
	sl := unsafe.Slice(arr, n)
	for i, v := range vals {
		sl[i] = C.CString(v)
	}
	return arr, func() {
		for i := range vals {
			C.free(unsafe.Pointer(sl[i]))
		}
		C.free(unsafe.Pointer(arr))
	}
}

// scan runs a scanner configured by setup and reads its hits. scoreCol names
// the column lance adds; negate turns a distance into a score.
func (s *Search) scan(what string, o store.SearchOptions, scoreCol string, negate bool, setup func(sc *C.LanceScanner) bool) ([]store.Hit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ds == nil {
		return nil, nil
	}
	cols, freeCols := cStrings(hitColumns)
	defer freeCols()
	var cFilter *C.char
	if f := filter(o); f != "" {
		cFilter = C.CString(f)
		defer C.free(unsafe.Pointer(cFilter))
	}
	var sc *C.LanceScanner
	out := C.amc_new_stream()
	defer C.free(unsafe.Pointer(out))
	err := call(what, func() bool {
		sc = C.lance_scanner_new(s.ds, cols, cFilter)
		if sc == nil {
			return false
		}
		if !setup(sc) {
			return false
		}
		if cFilter != nil && !ok(C.lance_scanner_set_prefilter(sc, C.bool(true))) {
			return false
		}
		return ok(C.lance_scanner_to_arrow_stream(sc, out))
	})
	if sc != nil {
		defer C.lance_scanner_close(sc)
	}
	if err != nil {
		return nil, err
	}
	rdr, err := cdata.ImportCRecordReader((*cdata.CArrowArrayStream)(unsafe.Pointer(out)), nil)
	if err != nil {
		return nil, fmt.Errorf("lance: %s: %w", what, err)
	}
	if r, ok := rdr.(interface{ Release() }); ok {
		defer r.Release()
	}
	var hits []store.Hit
	for {
		rec, err := rdr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("lance: %s: %w", what, err)
		}
		hits, err = appendHits(hits, rec, scoreCol, negate)
		if err != nil {
			return nil, fmt.Errorf("lance: %s: %w", what, err)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}

func appendHits(hits []store.Hit, rec arrow.RecordBatch, scoreCol string, negate bool) ([]store.Hit, error) {
	sch := rec.Schema()
	col := func(name string) arrow.Array {
		idx := sch.FieldIndices(name)
		if len(idx) == 0 {
			return nil
		}
		return rec.Column(idx[0])
	}
	// Arrow strings point into buffers lance-c owns and frees with the
	// batch, so every value is copied out.
	str := func(a arrow.Array, i int) string {
		switch v := a.(type) {
		case *array.String:
			return strings.Clone(v.Value(i))
		case *array.LargeString:
			return strings.Clone(v.Value(i))
		}
		return ""
	}
	id, run, turn, kind, text, emb := col("id"), col("run_id"), col("turn"), col("kind"), col("text"), col("embedder")
	if id == nil || run == nil || turn == nil || kind == nil || text == nil || emb == nil {
		return nil, fmt.Errorf("result is missing a column; got %v", sch)
	}
	score := col(scoreCol)
	for i := 0; i < int(rec.NumRows()); i++ {
		h := store.Hit{Chunk: store.Chunk{
			ID: str(id, i), RunID: str(run, i), Kind: str(kind, i), Text: str(text, i), Embedder: str(emb, i),
		}}
		if t, ok := turn.(*array.Int32); ok {
			h.Turn = int(t.Value(i))
		}
		if b := col("blob"); b != nil {
			h.Blob = blob.Hash(str(b, i))
		}
		if o, ok := col("offset").(*array.Int64); ok {
			h.Offset = o.Value(i)
		}
		switch v := score.(type) {
		case *array.Float32:
			h.Score = v.Value(i)
		case *array.Float64:
			h.Score = float32(v.Value(i))
		}
		if negate {
			h.Score = -h.Score
		}
		hits = append(hits, h)
	}
	return hits, nil
}

func limit(o store.SearchOptions) int {
	if o.Limit > 0 {
		return o.Limit
	}
	return 10
}

// FullText searches the text column with BM25.
func (s *Search) FullText(ctx context.Context, query string, o store.SearchOptions) ([]store.Hit, error) {
	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))
	cols, freeCols := cStrings([]string{"text"})
	defer freeCols()
	return s.scan("full-text search", o, "_score", false, func(sc *C.LanceScanner) bool {
		return ok(C.lance_scanner_full_text_search(sc, cQuery, cols, 0)) &&
			ok(C.lance_scanner_set_limit(sc, C.int64_t(limit(o))))
	})
}

// Nearest returns the chunks whose vectors are closest to vector.
func (s *Search) Nearest(ctx context.Context, vector []float32, o store.SearchOptions) ([]store.Hit, error) {
	if len(vector) != s.dim {
		return nil, fmt.Errorf("lance: query vector has %d dimensions, dataset has %d", len(vector), s.dim)
	}
	col := C.CString("vector")
	defer C.free(unsafe.Pointer(col))
	// The query is copied into C memory: a Go pointer must not outlive the call.
	q := (*C.float)(C.malloc(C.size_t(s.dim) * C.size_t(unsafe.Sizeof(C.float(0)))))
	defer C.free(unsafe.Pointer(q))
	copy(unsafe.Slice((*float32)(unsafe.Pointer(q)), s.dim), vector)
	return s.scan("vector search", o, "_distance", true, func(sc *C.LanceScanner) bool {
		if !ok(C.lance_scanner_nearest(sc, col, unsafe.Pointer(q), C.size_t(s.dim), C.LANCE_DTYPE_FLOAT32, C.uint32_t(limit(o)))) {
			return false
		}
		if !ok(C.lance_scanner_set_metric(sc, s.metric)) {
			return false
		}
		if o.Exact && !ok(C.lance_scanner_set_use_index(sc, C.bool(false))) {
			return false
		}
		if o.Probes > 0 && !ok(C.lance_scanner_set_nprobes(sc, C.uint32_t(o.Probes))) {
			return false
		}
		refine := o.Refine
		if refine == 0 {
			refine = store.DefaultRefine
		}
		if refine > 0 && !o.Exact && !ok(C.lance_scanner_set_refine_factor(sc, C.uint32_t(refine))) {
			return false
		}
		return true
	})
}

// Hybrid runs a full-text and a vector search and fuses them by reciprocal
// rank: a chunk's score is the sum of 1/(k + rank) over the lists it is in.
func (s *Search) Hybrid(ctx context.Context, query string, vector []float32, o store.SearchOptions) ([]store.Hit, error) {
	want := limit(o)
	wide := o
	wide.Limit = want * 4
	text, err := s.FullText(ctx, query, wide)
	if err != nil {
		return nil, err
	}
	near, err := s.Nearest(ctx, vector, wide)
	if err != nil {
		return nil, err
	}
	return Fuse(want, text, near), nil
}

// Fuse merges ranked lists by reciprocal rank and returns the best n.
func Fuse(n int, lists ...[]store.Hit) []store.Hit {
	byID := map[string]*store.Hit{}
	var order []string
	for _, list := range lists {
		for rank, h := range list {
			got, seen := byID[h.ID]
			if !seen {
				c := h
				c.Score = 0
				got = &c
				byID[h.ID] = got
				order = append(order, h.ID)
			}
			got.Score += 1 / float32(rrfK+rank+1)
		}
	}
	out := make([]store.Hit, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
