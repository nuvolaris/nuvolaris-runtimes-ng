/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package openwhisk

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// OutputGuard constant string
const OutputGuard = "XXX_THE_END_OF_A_WHISK_ACTIVATION_XXX\n"

// DefaultTimeoutStart to wait for a process to start
var DefaultTimeoutStart = 5 * time.Millisecond

// Executor is the container and the guardian  of a child process
// It starts a command, feeds input and output, read logs and control its termination
type Executor struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	exited chan bool
}

// NewExecutor creates a child subprocess using the provided command line,
// writing the logs in the given file.
// You can then start it getting a communication channel
func NewExecutor(logout *os.File, logerr *os.File, command string, args ...string) (proc *Executor) {
	cmd := exec.Command(command, args...)
	cmd.Stdout = logout
	cmd.Stderr = logerr
	cmd.Env = []string{
		"__OW_API_HOST=" + os.Getenv("__OW_API_HOST"),
	}
	Debug("env: %v", cmd.Env)
	if Debugging {
		cmd.Env = append(cmd.Env, "OW_DEBUG=/tmp/action.log")
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	pipeOut, pipeIn, err := os.Pipe()
	if err != nil {
		return nil
	}
	cmd.ExtraFiles = []*os.File{pipeIn}
	output := bufio.NewReader(pipeOut)
	return &Executor{
		cmd,
		input,
		output,
		make(chan bool),
	}
}

// Interact interacts with the underlying process
func (proc *Executor) Interact(in []byte) ([]byte, error) {
	// input to the subprocess
	proc.input.Write(in)
	proc.input.Write([]byte("\n"))
	out, err := proc.output.ReadBytes('\n')
	proc.cmd.Stdout.Write([]byte(OutputGuard))
	proc.cmd.Stderr.Write([]byte(OutputGuard))
	return out, err
}

// Exited checks if the underlying command exited
func (proc *Executor) Exited() bool {
	select {
	case <-proc.exited:
		return true
	default:
		return false
	}
}

// Start execution of the command
// wait a bit to check if the command exited
// returns an error if the program fails
func (proc *Executor) Start() error {
	// start the underlying executable
	Debug("Start:")
	err := proc.cmd.Start()
	if err != nil {
		Debug("run: early exit")
		proc.cmd = nil // no need to kill
		return fmt.Errorf("command exited")
	}
	Debug("pid: %d", proc.cmd.Process.Pid)
	go func() {
		proc.cmd.Wait()
		proc.exited <- true
	}()
	select {
	case <-proc.exited:
		return fmt.Errorf("command exited")
	case <-time.After(DefaultTimeoutStart):
		return nil
	}
}

// Stop will kill the process
// and close the channels
func (proc *Executor) Stop() {
	Debug("stopping")
	if proc.cmd != nil {
		proc.cmd.Process.Kill()
		proc.cmd = nil
	}
}