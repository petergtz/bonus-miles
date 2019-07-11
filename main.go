package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/fly/rc"
	"github.com/concourse/concourse/go-concourse/concourse"
	"golang.org/x/oauth2"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	targetName     = kingpin.Flag("target", "Concourse Target").Short('t').String()
	username       = kingpin.Flag("username", "Username").Short('u').String()
	password       = kingpin.Flag("password", "Password").Short('p').String()
	teamName       = kingpin.Flag("teamname", "Concourse team name (only needed when logging in with username and password)").String()
	url            = kingpin.Flag("url", "Concourse URL (only needed when logging in with username and password)").String()
	runLocally     = kingpin.Flag("local", "When set runs the server on 127.0.0.1:12345").Default("false").Short('l').Bool()
	shouldAutoOpen = kingpin.Flag("open", "Automatically open browser window.").Default("false").Short('a').Bool()
)

func main() {
	kingpin.Parse()
	if *targetName != "" && *username != "" {
		kingpin.FatalUsage("Please provide either --target or --username and --password, not both")
	}
	if *targetName == "" && *username == "" {
		kingpin.FatalUsage("Please provide either --target or --username and --password")
	}
	var (
		target rc.Target
		e      error
	)

	if *targetName == "" {
		target, e = rc.NewUnauthenticatedTarget("irrelevant-target-name", *url, *teamName, true, "", true)
		tokenType, accessToken, e := passwordGrant(target.Client(), *username, *password)
		Must(e)
		rc.SaveTarget(
			"bonus-miles",
			*url,
			true,
			*teamName,
			&rc.TargetToken{
				Type:  tokenType,
				Value: accessToken,
			},
			"",
		)
		*targetName = "bonus-miles"
	}
	target, e = rc.LoadTarget(rc.TargetName(*targetName), true)
	Must(e)

	fmt.Printf("Target: %#v\n\n", target)

	httpClient := target.Client().HTTPClient()

	http.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		generateOutput(httpClient, rw, req, target.URL())
	})
	addr := ":" + os.Getenv("PORT")
	if *runLocally {
		addr = "127.0.0.1:12345"
		go func() {
			time.Sleep(time.Second)
			fmt.Println("Server running at:", "http://"+addr)
			if *shouldAutoOpen {
				Must(exec.Command("open", "http://"+addr).Run())
			}
		}()
	}
	e = http.ListenAndServe(addr, nil)
	Must(e)
}

func passwordGrant(client concourse.Client, username, password string) (string, string, error) {
	oauth2Config := oauth2.Config{
		ClientID:     "fly",
		ClientSecret: "Zmx5",
		Endpoint:     oauth2.Endpoint{TokenURL: client.URL() + "/sky/token"},
		Scopes:       []string{"openid", "profile", "email", "federated:id", "groups"},
	}

	token, err := oauth2Config.PasswordCredentialsToken(
		context.WithValue(context.Background(), oauth2.HTTPClient, client.HTTPClient()),
		username, password)
	if err != nil {
		return "", "", err
	}
	return token.TokenType, token.AccessToken, nil
}

var urlPattern = regexp.MustCompile(`/api/v1/teams/(.*)/pipelines/(.*)/resources/(.*)/progress`)

func generateOutput(httpClient *http.Client, rw http.ResponseWriter, req *http.Request, apiURL string) {
	matches := urlPattern.FindStringSubmatch(req.URL.Path)
	if len(matches) != 4 {
		http.Error(rw, "Bad Request", http.StatusBadRequest)
		return
	}
	teamName := matches[1]
	pipeline := matches[2]
	resourceToTrack := matches[3]

	jobNames := strings.Split(req.URL.Query().Get("jobs"), "|")

	versionsURL := apiURL + "/api/v1/teams/" + teamName + "/pipelines/" + pipeline + "/resources/" + resourceToTrack + "/versions"

	resp, e := httpClient.Get(versionsURL + "?limit=5")
	Must(e)
	fmt.Println("Response Code:", resp.StatusCode)

	if resp.StatusCode == http.StatusUnauthorized {
		http.Error(rw, "You are not authorized. Please log in first, via fly login.", http.StatusUnauthorized)
		return
	}

	content, e := ioutil.ReadAll(resp.Body)
	Must(e)
	var vrs []atc.ResourceVersion
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

	fmt.Fprintln(rw, `<html>
	<head>
		<link rel="stylesheet" href="https://bootswatch.com/4/darkly/bootstrap.css" media="screen">
		<link rel="stylesheet" href="https://bootswatch.com/_assets/css/custom.min.css">
		<meta http-equiv="refresh" content="30"/>
	</head>
	<body>
	        <table class="table table-hover">`)

	fmt.Fprintln(rw, `<thead><tr><th scope="col">`+resourceToTrack+`</th><th scope="col">`, strings.Join(jobNames, `</th><th scope="col">`), `</th></tr></thead>`)
	for _, version := range versions {
		fmt.Println("GET", versionsURL+"/"+strconv.Itoa(versionIDs[version])+"/input_to", "...")
		resp, e = httpClient.Get(versionsURL + "/" + strconv.Itoa(versionIDs[version]) + "/input_to")
		Must(e)
		fmt.Println("Response Code:", resp.StatusCode)

		content, e = ioutil.ReadAll(resp.Body)
		Must(e)

		var builds []atc.Build
		Must(json.Unmarshal(content, &builds))
		buildStatuses := make(map[string]atc.BuildStatus)

		for _, build := range builds {
			if buildStatuses[build.JobName] != atc.StatusSucceeded {
				buildStatuses[build.JobName] = atc.BuildStatus(build.Status)
			}
		}
		fmt.Fprint(rw, "<tr><td>", version)
		for _, jobName := range jobNames {
			status := buildStatuses[jobName]
			if status == atc.StatusSucceeded {
				status = `<button type="button" class="btn btn-success"> </button>`
			}
			if status == atc.StatusFailed {
				status = `<button type="button" class="btn btn-danger"> </button>`
			}
			if status == atc.StatusStarted {
				status = `<button type="button" class="btn btn-warning"> </button>`
			}
			fmt.Fprint(rw, "</td><td>", status)
		}
		fmt.Fprintln(rw, "</td></tr>")
	}
	fmt.Fprintln(rw, `</table>
	</body>
	</html>`)
}

func Must(e error) {
	if e != nil {
		panic(e)
	}
}
