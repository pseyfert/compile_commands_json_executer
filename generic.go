/*
 * Copyright (c) 2019, CERN for the benefit of the LHCb collaboration
 * Author: Paul Seyfert <pseyfert@cern.ch>
 *
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are met:
 *     * Redistributions of source code must retain the above copyright
 *       notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above copyright
 *       notice, this list of conditions and the following disclaimer in the
 *       documentation and/or other materials provided with the distribution.
 *     * Neither the name of the <organization> nor the
 *       names of its contributors may be used to endorse or promote products
 *       derived from this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 * DISCLAIMED. IN NO EVENT SHALL <COPYRIGHT HOLDER> BE LIABLE FOR ANY
 * DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
 * LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
 * ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
 * SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/pflag"

	"github.com/phayes/permbits"
	"github.com/pseyfert/compilecommands_to_compilerexplorer/cc2ce"
	"github.com/pseyfert/go-workpool"
)

func main() {
	var infile string
	pflag.StringVar(&infile, "i", "./compile_commands.json", "input compile_commands.json database")
	appends := pflag.StringArray("extra-arg", []string{}, "additional arguments to append to the command line")
	prepends := pflag.StringArray("extra-arg-before", []string{}, "additional arguments to prepend to the command line")
	removeflag := pflag.StringArray("remove-filter", []string{}, "arguments to remove from the command line")
	exe := pflag.String("cmd", "", "command to run instead of the regular compiler")
	filterflag := pflag.String("filter", "", "source files to run on")
	env := pflag.StringToString("env", map[string]string{}, "prepend values to environment variables")
	_ = pflag.StringToString("replace", map[string]string{}, "replace arguments")
	concurrency := pflag.Int("j", 1, "concurrency")
	pflag.Parse()
	var err error

	remove := make([]*regexp.Regexp, 0, len(*removeflag))
	if len(*removeflag) > 0 {
		for _, r := range *removeflag {
			rexp, err := regexp.Compile(r)
			if err != nil {
				log.Printf("could not parse remove expression %s: %v", *removeflag, err)
				os.Exit(1)
			}
			remove = append(remove, rexp)
		}
	}

	var filter *regexp.Regexp
	if *filterflag != "" {
		filter, err = regexp.Compile(*filterflag)
		if err != nil {
			log.Printf("could not parse filter expression %s: %v", *filterflag, err)
			os.Exit(1)
		}
	} else {
		filter = nil
	}

	infile, err = filepath.Abs(infile)
	if err != nil {
		log.Printf("%v", err)
		return
	}
	dbdir := filepath.Dir(infile)

	jsonFile, err := os.Open(infile)
	if err != nil {
		log.Printf("sigh: %v", err)
		os.Exit(1)
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)

	all_parsedDB, err := ProcessJsonByBytes(byteValue, false, dbdir)
	use_parsedDB := make([]CompilerCall, 0, len(all_parsedDB))

	cmdpipe, outpipe := workpool.Workpool(*concurrency)

	for _, v := range all_parsedDB {
		if filter != nil {
			if !filter.MatchString(v.InFile) {
				continue
			}
		}
		if *exe != "" {
			v.Exe = []string{*exe}
		}

		if len(remove) > 0 {
			tmpargs := make([]string, 0, len(v.Args))
		argloop:
			for _, a := range v.Args {
				for _, r := range remove {
					if r.MatchString(a) {
						continue argloop
					}
				}
				tmpargs = append(tmpargs, a)
			}
			v.Args = tmpargs
		}

		v.Args = append(v.Args, *appends...)
		v.Args = append(*prepends, v.Args...)
		use_parsedDB = append(use_parsedDB, v)
	}

	myenv := os.Environ()
	newenv := make([]string, 0, len(myenv))
envloop:
	for _, e := range myenv {
		tmp := strings.Split(e, "=")
		k_e, v_e := tmp[0], tmp[1]
		for k_m, v_m := range *env {
			if k_m == k_e {
				newenv = append(newenv, fmt.Sprintf("%s=%s:%s", k_m, v_m, v_e))
				continue envloop
			}
		}
		newenv = append(newenv, fmt.Sprintf("%s=%s", k_e, v_e))
	}

	go func() {
		for _, v := range use_parsedDB {
			tmp := make([]string, 0, len(v.Exe)+len(v.Args))
			tmp = append(tmp, v.Exe[1:len(v.Exe)]...)
			tmp = append(tmp, v.Args...)
			cmd := exec.Command(v.Exe[0], tmp...)
			cmd.Env = newenv
			cmd.Dir = v.Dir
			cmdpipe <- cmd
		}
		close(cmdpipe)
	}()

	workpool.DrawProgress(outpipe, len(use_parsedDB))

}

type CompilerCall struct {
	Exe     []string // could be several words in case of ccache
	Args    []string
	Dir     string
	InFile  string
	OutFile string
}

func ProcessJsonByBytes(inFileContent []byte, turnAbsolute bool, dbdir string) ([]CompilerCall, error) {
	var db []cc2ce.JsonTranslationunit
	json.Unmarshal(inFileContent, &db)

	retval := make([]CompilerCall, 0, len(db))

	for _, tu := range db {
		still_exe := true
		skip_next := false
		words := strings.Fields(tu.Command)
		var call CompilerCall

		call.InFile = tu.File
		if !filepath.IsAbs(tu.Builddir) {
			call.Dir = filepath.Join(dbdir, tu.Builddir)
		} else {
			call.Dir = tu.Builddir
		}

		for j, w := range words {
			if skip_next {
				skip_next = false
				continue
			}
			if still_exe {
				call.Exe = append(call.Exe, w)
				if strings.HasSuffix(w, "ccache") {
					still_exe = true
				} else if next := words[j+1]; strings.HasPrefix(next, "-") {
					still_exe = false
				} else if _, err := os.Stat(next); os.IsNotExist(err) {
					still_exe = false
				} else if permissions, _ := permbits.Stat(next); permissions.UserExecute() {
					still_exe = true
					if tu.File == next { // executable input file?
						still_exe = false
					}
				} else {
					still_exe = false
				}
				continue
			}
			if w[0:2] == "-I" {
				inc := w[2:len(w)]
				if !filepath.IsAbs(inc) && turnAbsolute {
					inc = filepath.Join(call.Dir, inc)
				}
				call.Args = append(call.Args, fmt.Sprintf("-I%s", inc))
			} else if w == "-isystem" {
				inc := words[j+1]
				if !filepath.IsAbs(inc) && turnAbsolute {
					inc = filepath.Join(call.Dir, inc)
				}
				call.Args = append(call.Args, "-isystem", inc)
				skip_next = true
			} else if strings.HasPrefix(w, "-D") {
				call.Args = append(call.Args, strings.Replace(w, "\\\"", "\"", 2))
			} else if strings.HasPrefix(w, "-p") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-O") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-g") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-m") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-f") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-W") {
				call.Args = append(call.Args, w)
			} else if w == "-c" {
				call.Args = append(call.Args, w)
			} else if w == "-o" {
				call.Args = append(call.Args, w)
				next := words[j+1]
				if !filepath.IsAbs(next) && turnAbsolute {
					next = filepath.Join(call.Dir, next)
				}
				call.Args = append(call.Args, next)
				call.OutFile = next
				skip_next = true
			} else if strings.HasPrefix(w, "-std") {
				call.Args = append(call.Args, w)
			} else if strings.HasPrefix(w, "-M") {
				call.Args = append(call.Args, w)
				// unexpected in compile_commands.json
				if strings.HasPrefix(w, "-MT") || strings.HasPrefix(w, "-MF") || strings.HasPrefix(w, "-MQ") {
					next := words[j+1]
					if !filepath.IsAbs(next) && turnAbsolute {
						next = filepath.Join(call.Dir, next)
					}
					call.Args = append(call.Args, next)
					skip_next = true
				}
			} else if w == tu.File {
				call.Args = append(call.Args, w)
			} else {
				call.Args = append(call.Args, w)
				log.Printf("unknown compiler argument: %s", w)
			}
		}
		retval = append(retval, call)
	}
	return retval, nil
}
