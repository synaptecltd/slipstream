# Slipstream C/C++ interface

## Overview

The Go compiler has the interesting ability to target embedding Go code within a C/C++ program - with some caveats, of course. This means that a suitable Go API can be called from C code, allowing reuse of code and avoiding rewriting Go libraries for other languages.

This is achieved by adding `-buildmode=c-shared` to the `go build` command. This generates a C header file and an object file which can be easily linked within C/C++ code.

These are the main caveats with embedding Go programs within C/C++:
- The Go API should ideally involve only basic Go primitive types.
- You cannot easily exchange pointers, or any type which relies on pointers such as slices or structs, from Go to C code. Generally, it is easier for memory to be allocated in C code, and then accessed from Go.
- There are convenience functions such as `C.CBytes()` and `C.CString()` but these must be used with care to avoid leaking memory.

## Example

This directory contains an example of Go and C/C++ files which could be used to implement Slipstream within a C/C++ environment.

Run `build.sh` to test the interface.

## Further info

The following links provide useful information about this process:
- https://github.com/vladimirvivien/go-cshared-examples
- https://gist.github.com/helinwang/2c7bd2867ea5110f70e6431a7c80cd9b
- https://stackoverflow.com/questions/43646589/does-passing-a-slice-to-golang-from-c-do-a-memory-copy/43646947#43646947