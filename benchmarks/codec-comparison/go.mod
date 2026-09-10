module github.com/selawe/go-opus-codec/benchmarks/codec-comparison

go 1.26.2

require (
	github.com/hraban/opus v0.0.0-20260708213942-bde8e4304501
	github.com/pion/opus v0.1.0
	github.com/selawe/go-opus-codec v0.0.0-00010101000000-000000000000
)

replace github.com/selawe/go-opus-codec => ../..
