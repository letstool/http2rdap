@echo off
go build ^
    -trimpath ^
    -ldflags="-s -w" ^
    -tags netgo ^
    -o .\out\http2rdap.exe .\cmd\http2rdap
