// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/lrstanley/x/http/utils/httpccache"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	fsc, err := httpccache.NewFileStorage("cache", 256, 7*24*time.Hour)
	if err != nil {
		logger.Error("creating file storage", "error", err)
		os.Exit(1)
	}

	client := httpccache.NewClient(&httpccache.Config{
		Storage:  fsc,
		Logger:   logger,
		LogLevel: new(slog.LevelDebug),
	})

	username := "torvalds"
	url := "https://api.github.com/users/" + username

	ctx := context.Background()

	for i := range 10 {
		rlogger := logger.With("request", i+1)

		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			rlogger.ErrorContext(ctx, "creating request", "error", err)
			os.Exit(1)
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		var resp *http.Response
		resp, err = client.Do(req)
		if err != nil {
			rlogger.ErrorContext(ctx, "performing request", "error", err)
			os.Exit(1)
		}

		rlogger.InfoContext(
			ctx, "response",
			"status", resp.Status,
			"cache-status", httpccache.CacheStatusFromResponse(resp),
			"cache-reason", httpccache.CacheReasonFromResponse(resp),
			"from-cache", httpccache.IsFromCache(resp),
		)

		var user struct {
			Login       string `json:"login"`
			Name        string `json:"name"`
			Location    string `json:"location"`
			PublicRepos int    `json:"public_repos"`
		}

		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&user)
		if err != nil {
			rlogger.ErrorContext(ctx, "decoding response", "error", err)
			_ = resp.Body.Close()
			os.Exit(1)
		}
		_ = resp.Body.Close()

		rlogger.InfoContext(
			ctx, "response",
			"login", user.Login,
			"name", user.Name,
			"location", user.Location,
			"public-repos", user.PublicRepos,
		)
	}
}
