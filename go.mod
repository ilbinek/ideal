module arma3modmanager

go 1.25.0

require (
	github.com/mattn/go-sqlite3 v1.14.19
	golang.org/x/net v0.32.0
)

require (
	github.com/klauspost/compress v1.18.6 // indirect
	golang.org/x/sync v0.21.0 // indirect
)

require (
	github.com/0xAozora/go-steam v0.0.0-00010101000000-000000000000 // indirect
	github.com/itchio/lzma v0.0.0-20190703113020-d3e24e3e3d49 // indirect
	github.com/tbdevent/gosteam v0.0.0
	google.golang.org/protobuf v1.35.2 // indirect
)

// gosteam is vendored in-repo so it compiles into the binary (no external
// executable). Its own go.mod restates the go-steam fork replace, but replace
// directives in non-main modules are ignored, so we repeat it here.
replace (
	github.com/0xAozora/go-steam => ./third_party/gosteam/third_party/go-steam
	github.com/tbdevent/gosteam => ./third_party/gosteam
)
