This tool shall enable a user to run compilation commands from a
compile_commands.json file without messing with the original build tool.

Applications may be:

 - using a different compiler (e.g. clang++ instead of g++)
```
go run github.com/pseyfert/compile_commands_json_executer --cmd /path/to/clang++
```

 - add compiler arguments
```
go run github.com/pseyfert/compile_commands_json_executer --cmd /path/to/clang++ --extra-arg=--gcc-toolchain=--gcc-toolchain=/cvmfs/lhcb.cern.ch/lib/lcg/releases/gcc/7.3.0/x86_64-centos7
```

 - run other clang tools (kythe, include-what-you-use)
```
go run github.com/pseyfert/compile_commands_json_executer --cmd /opt/kythe/extractors/cxx_extractor --env LD_LIBRARY_PATH=/cvmfs/lhcb.cern.ch/lib/lcg/releases/clang/8.0.0/x86_64-centos7/lib64:/cvmfs/lhcb.cern.ch/lib/lcg/releases/gcc/8.2.0/x86_64-centos7/lib64:/cvmfs/lhcb.cern.ch/lib/lcg/releases/binutils/2.30/x86_64-centos7/lib${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}} --extra-arg=--gcc-toolchain=/cvmfs/lhcb.cern.ch/lib/lcg/releases/gcc/8.2.0/x86_64-centos7
```

 - or just disable compilation and only run preprocessors
```
go run github.com/pseyfert/compile_commands_json_executer --remove-filter='-c' --append='-E'
```

## what's there and what isn't

### what's there

 - concurrency: `-j 4` to run with 4 thread
 - relative path handling: the working directory of the compiler call shall be the one specified in the compilation database. Relative paths therein should just work.
 - limitted compiler launcher tool detection: when using a compiler launcher in the database like `ccache g++`, the combination of them is detected as "executable". It won't be replaced to `cxx_extractor g++` but instead to `cxx_extractor`.
 - many arguments can occur repeatedly, such as `--extra-arg` and `--extra-arg-before` to add multiple arguments (they maintain the order in which they appear, i.e. the first `--extra-arg-before` will be the first argument)
 - `--trace` will write a json file that can be opened with the chrome webbrowser on the `chrome://tracing` page to visualize which processes launch when.
 - see also the output of `--help`

### what's not there

 - multi word argument treatment: file name replacements for output files (e.g. `-o /some/outfile` or `-MF /some/other`) can not be done as the `replace` and `remove-filter` options go through the command line word by word and can't jump over the space after `-o`
 - compiler launchers for the replacement executable (i.e. `--cmd="ccache clang++"` won't work). But they can be worked around with `--cmd="ccache" --extra-arg-before "clang++"`
