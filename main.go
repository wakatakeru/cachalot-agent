package main

import (
	"bytes"
	"encoding/json"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
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
}

type Result struct {
	ID     string `json:"id"`
	Stdout string `json:"stdout"`
	Data   string `json:"result_data"`
}

var ctx = context.Background()
var cli, _ = client.NewClientWithOpts(client.WithVersion("1.39"))

func isCorrectRequest(r *http.Request) error {
	if r.Method != "POST" {
		return errors.New("Method is not POST")
	}

	if _, _, err := r.FormFile("recipe"); err != nil {
		return err
	}

	if _, _, err := r.FormFile("recipe"); err != nil {
		return err
	}

	return nil
}

func unmarshalRecipeJSON(recipeBytes []byte, recipeJSON *Recipe) error {
	if err := json.Unmarshal(recipeBytes, &recipeJSON); err != nil {
		return err
	}

	if recipeJSON.Command == nil || recipeJSON.Image == "" {
		return errors.New("Can not parse Recipe JSON")
	}

	return nil
}

func paramsString(r *http.Request) (data multipart.File, recipe multipart.File, err error) {
	dataString, _, err := r.FormFile("data")
	defer dataString.Close()

	recipeString, _, err := r.FormFile("recipe")
	defer recipeString.Close()

	return dataString, recipeString, err
}

func execHandler(w http.ResponseWriter, r *http.Request) {
	if err := isCorrectRequest(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tempDir, err := ioutil.TempDir("", "peb-agent")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	dataString, recipeString, _ := paramsString(r)
	recipeBytes, _ := ioutil.ReadAll(io.Reader(recipeString))

	var recipe Recipe
	if err = unmarshalRecipeJSON(recipeBytes, &recipe); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f, err := os.Create(tempDir + "/data.tar")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	io.Copy(f, dataString)

	exec.Command("tar", "xvf", tempDir+"/data.tar", "-C", tempDir).Run()

	command := recipe.Command
	image := recipe.Image

	resultStdout := containerExecutor(image, command, tempDir, w)

	os.Chdir(tempDir)
	os.Remove(tempDir + "/data.tar")
	exec.Command("tar", "cvf", "result.tar", ".").Run()

	resultData, err := ioutil.ReadFile(tempDir + "/result.tar") // TODO: resultをサーブする

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var result Result
	result.ID = "pending" // TODO: pending
	result.Stdout = resultStdout
	result.Data = base64.StdEncoding.EncodeToString(resultData)
	
	resultJSONBytes, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%v\n", string(resultJSONBytes))
}

func containerExecutor(imageName string, command []string, workDir string, w http.ResponseWriter) string {
	_, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return "error"
	}

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
		Binds: []string{workDir + ":/tmp"},
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
		panic(err)
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
