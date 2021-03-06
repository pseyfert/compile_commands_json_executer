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

package compile_commands_json_executer

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/phayes/permbits"
	"github.com/pseyfert/compilecommands_to_compilerexplorer/cc2ce"
	"github.com/pseyfert/go-workpool"
)

type Executer struct {
	Appends     []string
	Prepends    []string
	RemoveArgs  []string
	Exe         string
	AcceptTU    []string
	RejectTU    []string
	Env         map[string]string
	Replace     map[string]string
	Concurrency int
	TraceFile   string
}

type DecoratedRun struct {
	realcmd *exec.Cmd
	name    string
}

func (d DecoratedRun) Cat() string {
	return "target"
}

func (d *DecoratedRun) Name() string {
	return d.name
}

func (d *DecoratedRun) Run() error {
	return d.realcmd.Run()
}

func (d *DecoratedRun) SetStderr(w io.Writer) {
	d.realcmd.Stderr = w
}

func (d *DecoratedRun) SetStdout(w io.Writer) {
	d.realcmd.Stdout = w
}

func (e *Executer) Run(infile string) error {
	var err error
	var tracefile io.Writer = nil
	if e.TraceFile != "" {
		f, err := os.Create(e.TraceFile)
		tracefile = f
		if err != nil {
			return fmt.Errorf("couldn't create trace file: %v\n", err)
		}
	}
	remove := make([]*regexp.Regexp, 0, len(e.RemoveArgs))
	if len(e.RemoveArgs) > 0 {
		for _, r := range e.RemoveArgs {
			rexp, err := regexp.Compile(r)
			if err != nil {
				return fmt.Errorf("could not parse remove expression %s: %v", e.RemoveArgs, err)
			}
			remove = append(remove, rexp)
		}
	}

	rejectfilter := make([]*regexp.Regexp, 0, len(e.RejectTU))
	if len(e.RejectTU) != 0 {
		for _, r := range e.RejectTU {
			rf, err := regexp.Compile(r)
			if err != nil {
				return fmt.Errorf("could not parse reject filter expression %s: %v", r, err)
			}
			rejectfilter = append(rejectfilter, rf)
		}
	}

	acceptfilter := make([]*regexp.Regexp, 0, len(e.AcceptTU))
	if len(e.AcceptTU) != 0 {
		for _, a := range e.AcceptTU {
			af, err := regexp.Compile(a)
			if err != nil {
				return fmt.Errorf("could not parse accept filter expression %s: %v", a, err)
			}
			acceptfilter = append(acceptfilter, af)
		}
	}

	replaces := make(map[*regexp.Regexp]string)
	for k, v := range e.Replace {
		re, err := regexp.Compile(k)
		replaces[re] = v
		if err != nil {
			return fmt.Errorf("could not parse replacement expression %s: %v", k, err)
		}
	}

	infile, err = filepath.Abs(infile)
	if err != nil {
		return fmt.Errorf("%v", err)
	}
	dbdir := filepath.Dir(infile)

	jsonFile, err := os.Open(infile)
	if err != nil {
		return fmt.Errorf("couldn't open input compile_commands.json file: %v", err)
	}
	defer jsonFile.Close()
	log.Printf("opened %s for processing\n", infile)
	byteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return fmt.Errorf("couldn't read input compile_commands.json file: %v", err)
	}

	all_parsedDB, err := ProcessJsonByBytes(byteValue, false, dbdir)
	use_parsedDB := make([]CompilerCall, 0, len(all_parsedDB))

	cmdpipe, outpipe := workpool.Workpool(e.Concurrency, tracefile)

dbloop:
	for _, v := range all_parsedDB {
		if len(acceptfilter) != 0 {
			if nomatch(acceptfilter, v.InFile) {
				continue dbloop
			}
		}
		if len(rejectfilter) != 0 {
			if anymatch(rejectfilter, v.InFile) {
				continue dbloop
			}
		}
		if e.Exe != "" {
			v.Exe = []string{e.Exe}
		}

		if len(remove) > 0 {
			tmpargs := make([]string, 0, len(v.Args))
		argloop:
			for _, a := range v.Args {
				if anymatch(remove, a) {
					continue argloop
				}
				tmpargs = append(tmpargs, a)
			}
			v.Args = tmpargs
		}

		if len(replaces) > 0 {
			for i, arg := range v.Args {
				for exp, repl := range replaces {
					v.Args[i] = exp.ReplaceAllString(arg, repl)
				}
			}
		}

		v.Args = append(v.Args, e.Appends...)
		v.Args = append(e.Prepends, v.Args...)
		use_parsedDB = append(use_parsedDB, v)
	}

	myenv := os.Environ()
	if len(e.Env) > 0 {
		newenv := make([]string, 0, len(myenv))
		checkmarks := make(map[string]bool)
	envloop:
		for _, entry := range myenv {
			tmp := strings.Split(entry, "=")
			k_e, v_e := tmp[0], tmp[1]
			for k_m, v_m := range e.Env {
				if k_m == k_e {
					newenv = append(newenv, fmt.Sprintf("%s=%s:%s", k_m, v_m, v_e))
					checkmarks[k_m] = true
					continue envloop
				}
			}
			newenv = append(newenv, fmt.Sprintf("%s=%s", k_e, v_e))
		}
		for k_m, v_m := range e.Env {
			if _, ok := checkmarks[k_m]; !ok {
				newenv = append(newenv, fmt.Sprintf("%s=%s", k_m, v_m))
			}
		}
		myenv = newenv
	}

	go func() {
		for _, v := range use_parsedDB {
			tmp := make([]string, 0, len(v.Exe)+len(v.Args))
			tmp = append(tmp, v.Exe[1:len(v.Exe)]...)
			tmp = append(tmp, v.Args...)
			cmd := exec.Command(v.Exe[0], tmp...)
			cmd.Env = myenv
			cmd.Dir = v.Dir
			sendout := DecoratedRun{realcmd: cmd, name: v.OutFile}
			cmdpipe <- &sendout
		}
		close(cmdpipe)
	}()

	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		workpool.DrawProgress(outpipe, len(use_parsedDB))
	} else {
		workpool.DefaultPrint(outpipe)
	}
	return err
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
