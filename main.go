package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/fly/rc"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	targetName      = kingpin.Flag("target", "Concourse Target").Required().Short('t').String()
	resourceToTrack = kingpin.Flag("resource", "Resource to track").Required().Short('r').String()
	pipeline        = kingpin.Flag("pipeline", "Pipeline").Required().Short('p').String()
	shouldAutoOpen  = kingpin.Flag("open", "Automatically open the markdown file in the application associated with the *.md file extension.").Default("false").Short('a').Bool()
)

func main() {
	kingpin.Parse()
	input, e := ioutil.ReadAll(os.Stdin)
	Must(e)
	jobsNames := strings.Split(string(input), "\n")

	target, e := rc.LoadTarget(rc.TargetName(*targetName), true)

	Must(e)
	httpClient := target.Client().HTTPClient()

	http.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		generateOutput(httpClient, jobsNames, rw, target.URL()+"/api/v1/teams/"+target.Team().Name()+"/pipelines/"+*pipeline+"/resources/"+*resourceToTrack+"/versions")
	})
	addr := "127.0.0.1:12345"
	go func() {
		time.Sleep(time.Second)
		fmt.Println("Server running at:", "http://"+addr)
		if *shouldAutoOpen {
			Must(exec.Command("open", "http://"+addr).Run())
		}
	}()
	e = http.ListenAndServe(addr, nil)
	Must(e)
}

func generateOutput(httpClient *http.Client, jobsNames []string, tempFile io.Writer, versionsURL string) {
	resp, e := httpClient.Get(versionsURL)
	Must(e)

	content, e := ioutil.ReadAll(resp.Body)
	Must(e)
	var vrs []atc.VersionedResource
	Must(json.Unmarshal(content, &vrs))

	if len(vrs) == 0 {
		fmt.Println("No versioned resources")
		return
	}
	versionIDs := make(map[string]int)
	var versions []string
	for _, vr := range vrs {
		v := fmt.Sprintf("%#v", vr.Version)
		versionIDs[v] = vr.ID
		versions = append(versions, v)
	}

	fmt.Fprintln(tempFile, `<html>
	<head>
		<link rel="stylesheet" href="https://bootswatch.com/4/darkly/bootstrap.css" media="screen">
		<link rel="stylesheet" href="https://bootswatch.com/_assets/css/custom.min.css">
		<meta http-equiv="refresh" content="30"/>
	</head>
	<body>
	        <table class="table table-hover">`)

	fmt.Fprintln(tempFile, `<thead><tr><th scope="col">version</th><th scope="col">`, strings.Join(jobsNames, `</th><th scope="col">`), `</th></tr></thead>`)
	for _, version := range versions {
		resp, e = httpClient.Get(versionsURL + "/" + strconv.Itoa(versionIDs[version]) + "/input_to")
		Must(e)
		content, e = ioutil.ReadAll(resp.Body)
		Must(e)

		var builds []atc.Build
		Must(json.Unmarshal(content, &builds))
		buildSet := make(map[string]string)

		for _, build := range builds {
			if buildSet[build.JobName] != "succeeded" {
				buildSet[build.JobName] = build.Status
			}
		}
		fmt.Fprint(tempFile, "<tr><td>", version)
		for _, jobName := range jobsNames {
			status := buildSet[jobName]
			if status == "succeeded" {
				status = `<button type="button" class="btn btn-success"> </button>`
			}
			if status == "failed" {
				status = `<button type="button" class="btn btn-danger"> </button>`
			}
			fmt.Fprint(tempFile, "</td><td>", status)
		}
		fmt.Fprintln(tempFile, "</td></tr>")
	}
	fmt.Fprintln(tempFile, `</table>
	</body>
	</html>`)
}

func Must(e error) {
	if e != nil {
		panic(e)
	}
}
