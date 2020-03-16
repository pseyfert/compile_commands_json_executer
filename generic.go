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
	"log"

	"github.com/spf13/pflag"

	lib "github.com/pseyfert/compile_commands_json_executer/lib"
)

func main() {
	var infile string
	pflag.StringVar(&infile, "i", "./compile_commands.json", "input compile_commands.json database")
	appends := pflag.StringArray("extra-arg", []string{}, "additional arguments to append to the command line")
	prepends := pflag.StringArray("extra-arg-before", []string{}, "additional arguments to prepend to the command line")
	removeflag := pflag.StringArray("remove-filter", []string{}, "arguments to remove from the command line")
	exe := pflag.String("cmd", "", "command to run instead of the regular compiler")
	acceptfilterflag := pflag.StringArray("accept-filter", []string{}, "source files to run on (regex, can be used multiple times, must match at least one regex for acceptance)")
	rejectfilterflag := pflag.StringArray("reject-filter", []string{}, "source files not to run on (regex, can be used multiple times, must match at least one regex for rejection, rejection trumps acceptance)")
	env := pflag.StringToString("env", map[string]string{}, "prepend values to environment variables")
	replaceflag := pflag.StringToString("replace", map[string]string{}, "replace arguments")
	concurrency := pflag.Int("j", 1, "concurrency")
	tracefname := pflag.String("trace", "", "trace filename (won't trace if empty string)")
	pflag.Parse()

	executer := lib.Executer{
		Appends:     *appends,
		Prepends:    *prepends,
		RemoveArgs:  *removeflag,
		Exe:         *exe,
		AcceptTU:    *acceptfilterflag,
		RejectTU:    *rejectfilterflag,
		Env:         *env,
		Replace:     *replaceflag,
		Concurrency: *concurrency,
		TraceFile:   *tracefname,
	}

	err := executer.Run(infile)
	if err != nil {
		log.Printf("%v\n", err)
	}
}
