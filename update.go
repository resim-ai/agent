package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/google/go-github/v66/github"
	"golang.org/x/mod/semver"
)

func doUpdate(release *github.RepositoryRelease) error {
	var downloadURL string

	desiredFilename := fmt.Sprintf("agent-%v-%v", runtime.GOOS, runtime.GOARCH)

	for _, asset := range release.Assets {
		if *asset.Name == desiredFilename {
			downloadURL = *asset.URL
		}
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		slog.Error("error constructing update request", "err", err)
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")

	// REMOVE ONCE THE REPO IS PUBLIC
	req.Header.Set("Authorization", "Bearer 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")
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("error requesting update", "err", err)
		return err
	}

	if resp.StatusCode != 200 {
		slog.Debug("couldn't get release download", "status", resp.StatusCode, "url", downloadURL)
	}

	dlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error reading response", "err", err)
		return err
	}

	currentFilePath, err := os.Executable()
	if err != nil {
		slog.Error("error getting current file path", "err", err)
		return err
	}

	currentFileInfo, err := os.Stat(currentFilePath)
	if err != nil {
		slog.Error("error getting current file info", "err", err)
		return err
	}

	newFilePath := currentFilePath + "-new"
	oldFilePath := currentFilePath + "-old"

	err = os.WriteFile(newFilePath, dlBytes, currentFileInfo.Mode())
	if err != nil {
		slog.Error("error writing new version", "err", err)
		return err
	}

	os.Rename(currentFilePath, oldFilePath)
	err = os.Rename(newFilePath, currentFilePath)
	if err == nil {
		os.Remove(oldFilePath)
		slog.Info("new release downloaded, please restart the agent")
		os.Exit(0)
	}

	// defer resp.Body.Close()
	// err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	// if err != nil {
	// 	slog.Error("error applying update", "err", err)
	// 	return err
	// }

	return nil
}

func (a *Agent) checkUpdate() error {
	ctx := context.Background()
	gh := github.NewClient(nil).WithAuthToken("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")

	latestRelease, _, err := gh.Repositories.GetLatestRelease(ctx, "resim-ai", "agent")
	if err != nil {
		slog.Error("error getting latest release", "err", err)
		return err
	}

	// client := http.Client{
	// 	Timeout: 5 * time.Second,
	// }

	// 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
	// req, _ := http.NewRequest("GET", latestReleaseURL, nil)

	// REMOVE ONCE THE REPO IS PUBLIC
	// req.Header.Set("Authorization", "Bearer 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")
	// resp, err := client.Do(req)
	// if err != nil {
	// 	slog.Error("error checking for releases", "err", err)
	// 	return err
	// }

	// // var latestRelease githubRelease

	// bodyData, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	slog.Error("error reading release response body", "err", err)
	// 	return err
	// }

	// err = json.Unmarshal(bodyData, &latestRelease)
	// if err != nil {
	// 	slog.Error("error unmarshalling release response", "err", err)
	// 	return err
	// }

	switch semver.Compare(agentVersion, *latestRelease.Name) {
	case -1:
		slog.Info("there is a newer version of the agent available", "available_version", *latestRelease.Name, "running_version", agentVersion)
		if a.AutoUpdate {
			slog.Debug("attempting automatic update")
			err = doUpdate(latestRelease)
			if err != nil {
				slog.Error("error in automatic update", "err", err)
				return err
			}
		}
	case 0:
		slog.Debug("running the latest release", "version", agentVersion)
	case 1:
		slog.Debug("running a pre-release version", "available_version", *latestRelease.Name, "running_version", agentVersion)
	}

	return nil
}
