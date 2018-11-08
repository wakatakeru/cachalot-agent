package main

import (
  "encoding/json"
	"encoding/base64"
  "strconv"
	"crypto/rand"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"fmt"
	"time"
	"net/http"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"
)

type Recipe struct {
	Command []string `json:"command"`
	Image string `json:"image"`
	Data string `json:"data"`
}

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
}

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

func execHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	length, err := strconv.Atoi(r.Header.Get("Content-Length"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	
	body := make([]byte, length)
	body, err = ioutil.ReadAll(r.Body)
	if err != nil && err != io.EOF {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var jsonBody Recipe
	err = json.Unmarshal(body[:length], &jsonBody)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if jsonBody.Data == ""  || jsonBody.Command == nil || jsonBody.Image == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	
	data, _ := base64.StdEncoding.DecodeString(jsonBody.Data)
	execName := secureRandomStr(8)
	
	if err := os.Mkdir("./tmp/" + execName, 0777); err != nil {
		fmt.Println(err)
	}
	
	ioutil.WriteFile("./tmp/" + execName + "/data.tar", data, 0755)

	command := jsonBody.Command
	image := jsonBody.Image

	result := containerExecutor(image, command, execName)
	os.RemoveAll("./tmp/" + execName)

	fmt.Fprintf(w, "%v\n", result)
}

func containerExecutor(imageName string, command []string, execName string) string {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.37"))
	if err != nil {
		panic(err)
	}

	reader, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, reader)		

	currentDir, _ := os.Getwd()

	fileDir := currentDir + "/tmp/" + execName
	exec.Command("tar", "xvf", fileDir + "/data.tar", "-C", fileDir).Run()
	
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		WorkingDir: "/tmp",
		Cmd:   command, //strings.Split(command[0], " ")[:],  // []string{"echo", "hello"},
		Tty:   true,
	},&container.HostConfig{
		Binds: []string{fileDir + ":/tmp"},
	}, nil, "")
	
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

	return newStr
}

func main() {
	http.HandleFunc("/", cpuHandler)
	http.HandleFunc("/cpu", cpuHandler)
	http.HandleFunc("/mem", memHandler)
	http.HandleFunc("/container", execHandler)

	http.ListenAndServe(":8080", nil)
}
