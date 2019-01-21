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
	tempFile, e := ioutil.TempFile("", "bonus-miles-*.md")
	Must(e)
	defer tempFile.Close()

	generateOutput(httpClient, jobsNames, tempFile, target.URL()+"/api/v1/teams/"+target.Team().Name()+"/pipelines/"+*pipeline+"/resources/"+*resourceToTrack+"/versions")
	if *shouldAutoOpen {
		Must(exec.Command("open", tempFile.Name()).Run())
	} else {
		fmt.Println("Markdown can be found at:", tempFile.Name())
	}
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

	fmt.Fprintln(tempFile, "version|", strings.Join(jobsNames, "|"))
	fmt.Fprintln(tempFile, strings.Repeat("---|", len(jobsNames))+"---")
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
		fmt.Fprint(tempFile, version)
		for _, jobName := range jobsNames {
			status := buildSet[jobName]
			if status == "succeeded" {
				status = "<span style=\"color:green\">✔</span>"
			}
			if status == "failed" {
				status = "<span style=\"color:red\">!</span>"
			}
			fmt.Fprint(tempFile, "|", status)
		}
		fmt.Fprintln(tempFile)
	}
}

func Must(e error) {
	if e != nil {
		panic(e)
	}
}
