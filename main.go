package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/wakatakeru/peb-agent/handler/status/cpu"
	"github.com/wakatakeru/peb-agent/handler/status/health"
	"github.com/wakatakeru/peb-agent/handler/status/load"
	"github.com/wakatakeru/peb-agent/handler/status/memory"

	"golang.org/x/net/context"
)

type Recipe struct {
	Command []string `json:"command"`
	Image   string   `json:"image"`
	Data    string   `json:"data"`
}

type Result struct {
	Stdout string `json:"stdout"`
	Data   string `json:"data"`
}

var ctx = context.Background()
var cli, _ = client.NewClientWithOpts(client.WithVersion("1.39"))

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	body := make([]byte, length)
	body, err = ioutil.ReadAll(r.Body)
	if err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var jsonBody Recipe
	err = json.Unmarshal(body[:length], &jsonBody)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if jsonBody.Data == "" || jsonBody.Command == nil || jsonBody.Image == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	data, _ := base64.StdEncoding.DecodeString(jsonBody.Data)
	execName := secureRandomStr(8)
	currentDir, _ := os.Getwd()
	dir := currentDir+"/tmp/"+execName
	
	if err := os.Mkdir(dir, 0777); err != nil {
		fmt.Println(err)
	}
	// defer os.RemoveAll(dir)
	
	ioutil.WriteFile(dir+"/data.tar", data, 0755)
	exec.Command("tar", "xvf", dir+"/data.tar", "-C", dir).Run()
	
	command := jsonBody.Command
	image := jsonBody.Image

	result := containerExecutor(image, command, execName, w)

	if err := os.Remove(dir+"/data.tar"); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	var resultBody Result;

	os.Chdir(dir)
	exec.Command("tar", "cvf", "result.tar", ".").Run()
	os.Chdir(currentDir)
	
	buf, err := ioutil.ReadFile(dir+"/result.tar")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	
	resultData := base64.StdEncoding.EncodeToString(buf)
	
	resultBody.Stdout = result
	resultBody.Data = resultData

	resultJsonBytes, err := json.Marshal(resultBody)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%v\n", string(resultJsonBytes))
}

func containerExecutor(imageName string, command []string, execName string, w http.ResponseWriter) string {
	reader, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}
	io.Copy(os.Stdout, reader)

	currentDir, _ := os.Getwd()

	fileDir := currentDir + "/tmp/" + execName

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		WorkingDir: "/tmp",
		Cmd:        command,
		Tty:        true,
		Healthcheck: &container.HealthConfig{
			Test:     []string{"sh", "-c", "curl -f http://localhost/ || exit 1"},
			Interval: 1 * time.Second,
			Retries:  10,
		},
	}, &container.HostConfig{
		Binds: []string{fileDir + ":/tmp"},
	}, nil, "")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return "error"
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

	if err := cli.ContainerStop(ctx, resp.ID, nil); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

	if err := cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

	buf := new(bytes.Buffer)
	io.Copy(buf, out)
	newStr := buf.String()

	return newStr
}

func main() {
	http.HandleFunc("/", health.GetStatus)
	http.HandleFunc("/cpu", cpu.GetUsedPercent)
	http.HandleFunc("/memory", memory.GetUsedPercent)
	http.HandleFunc("/healthy", health.GetStatus)
	http.HandleFunc("/load", load.GetLoad1)
	http.HandleFunc("/container", execHandler)

	http.ListenAndServe(":8080", nil)
}
