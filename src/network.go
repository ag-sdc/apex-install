package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func doReq(cfg *RepoConfig, urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	} else if cfg.AuthBasic != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.AuthBasic))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	client := &http.Client{}
	return client.Do(req)
}

func tryDownload(repo *RepoConfig, syncDir, suffix string, fallbacks []string) error {
	for _, s := range fallbacks {
		urlStr := fmt.Sprintf("%s/%s%s", strings.TrimRight(repo.URL, "/"), repo.Name, s)
		resp, err := doReq(repo, urlStr)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			dest := filepath.Join(syncDir, repo.Name+suffix)
			f, err := os.Create(dest)
			if err != nil {
				resp.Body.Close()
				return err
			}
			io.Copy(f, resp.Body)
			f.Close()
			resp.Body.Close()
			return nil
		}
		resp.Body.Close()
	}
	return fmt.Errorf("could not download %s (404 Not Found)", suffix)
}
