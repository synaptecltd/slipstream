#!/bin/bash

# Go build
go build -buildmode=c-shared c-main.go

# compile C program
g++ -o c-lib main.c ./c-main -O3

# run
./c-lib