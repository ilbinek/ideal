module github.com/tbdevent/gosteam

go 1.25.0

// We vendor the go-steam fork under third_party so we own and can patch it
// (zstd chunk support, CDN auth tokens, etc.) while keeping its import paths.
replace github.com/0xAozora/go-steam => ./third_party/go-steam

require (
	github.com/0xAozora/go-steam v0.0.0-00010101000000-000000000000
	github.com/itchio/lzma v0.0.0-20190703113020-d3e24e3e3d49
	github.com/klauspost/compress v1.18.6
	golang.org/x/sync v0.21.0
	golang.org/x/term v0.27.0
	google.golang.org/protobuf v1.35.2
)

require (
	golang.org/x/net v0.32.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
)
