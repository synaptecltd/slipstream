#!/bin/bash

# Go build
# go tool cgo -godefs c-interface.go
go build -buildmode=c-shared c-interface.go

# compile C program
g++ -o c-lib main.cpp ./c-interface -O3

# run
./c-lib