#!/bin/bash

# Go build
# go tool cgo -godefs c-main.go
go build -buildmode=c-shared c-main.go

# compile C program
g++ -o c-lib main.cpp ./c-main -O3

# run
./c-lib