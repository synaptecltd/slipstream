#!/bin/bash

# Go build
go build -buildmode=c-shared c-main.go

# compile C program
gcc -o c-lib main.c ./c-main

# run
./c-lib