module arma3modmanager

go 1.25.1

godebug multipathtcp=0

require (
	github.com/ilbinek/godishook v1.0.0
	github.com/mattn/go-sqlite3 v1.14.19
	github.com/shirou/gopsutil/v4 v4.26.6
	golang.org/x/net v0.32.0
	golang.org/x/sys v0.41.0
)

require (
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
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
