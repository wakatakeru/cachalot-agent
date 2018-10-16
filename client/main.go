package main

import (
	"bytes"
	"io"
	"os"
	"fmt"
	"strings"
	"time"
	"net/http"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"
)

func cpuHandler(w http.ResponseWriter, r *http.Request) {
	v, _ := cpu.Percent(time.Duration(1 * time.Second), true)
	sum := 0.0

	for i := 0; i < len(v); i++ {
		sum += v[i]
	}
	
	fmt.Fprintf(w, "%f\n", sum / float64(len(v)))
}

func memHandler(w http.ResponseWriter, r *http.Request) {
	v, _ := mem.VirtualMemory()
	fmt.Fprintf(w, "%f\n", v.UsedPercent)
}

func containerHandler(w http.ResponseWriter, r *http.Request) {
	command, _ := r.URL.Query()["command"]
	fmt.Fprintf(w, "%s\n", string(command[0]))

	fmt.Printf("Command:%v\n", command)
	
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.37"))
	if err != nil {
		panic(err)
	}

	reader, err := cli.ImagePull(ctx, "docker.io/library/alpine", types.ImagePullOptions{})
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, reader)		

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine",
		Cmd:   strings.Split(command[0], " ")[:],  // []string{"echo", "hello"},
		Tty:   true,
	}, nil, nil, "")
	
	if err != nil {
		panic(err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		panic(err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	buf := new(bytes.Buffer)
	io.Copy(buf, out)
	newStr := buf.String()
	
	fmt.Fprintf(w, "Result: %v\n", newStr)
}

func main() {
	http.HandleFunc("/", cpuHandler)
	http.HandleFunc("/cpu", cpuHandler)
	http.HandleFunc("/mem", memHandler)
	http.HandleFunc("/container", containerHandler)

	http.ListenAndServe(":8080", nil)
}
