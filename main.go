// Copyright (c) 2022 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
	"github.com/google/uuid"
)

type incomingWebhook struct {
	Timestamp string            `json:"timestamp"`
	Version   int               `json:"version"`
	Type      string            `json:"type"`
	Tailnet   string            `json:"tailnet"`
	Message   string            `json:"message"`
	Data      map[string]string `json:"data"`
}

// https://learn.microsoft.com/en-us/outlook/actionable-messages/message-card-reference
type teamsWebhook struct {
    Type          string        `json:"@type"`
    Context       string        `json:"@context"`
    CorrelationId string        `json:"correlationId"`
    Text          string        `json:"text"`
    Summary       string        `json:"summary"`
    ThemeColor    string        `json:"themeColor"`
    Title         string        `json:"title"`
    Attachments   []attachment  `json:"attachments"`
}

type attachment struct {
    ContentType string                 `json:"contentType"`
    Content     map[string]interface{} `json:"content"`
}

func sendTeamsWebhook(orig incomingWebhook) {
    webhookUrl := os.Getenv("TEAMS_WEBHOOK_URL")
    if webhookUrl == "" {
        return
    }

    // Create the adaptive card content
    content := map[string]interface{}{
        "type": "AdaptiveCard",
        "body": []map[string]interface{}{
            {
                "type": "TextBlock",
                "size": "Medium",
                "weight": "Bolder",
                "text": orig.Message,
            },
            {
                "type": "FactSet",
                "facts": createFacts(orig.Data),
            },
        },
        "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
        "version": "1.2",
    }

    teams := teamsWebhook{
        Type:          "MessageCard",
        Context:       "https://schema.org/extensions",
        CorrelationId: uuid.NewString(),
        Summary:       orig.Message,
        ThemeColor:    "0078D7", // Microsoft blue
        Title:         orig.Message,
        Attachments: []attachment{
            {
                ContentType: "application/vnd.microsoft.card.adaptive",
                Content:     content,
            },
        },
    }

    body, err := json.Marshal(teams)
    if err != nil {
        log.Printf("[%s] sendTeamsWebhook json.Marshal failed: %v", time.Now().Format(time.RFC3339), err)
        return
    }

    // Rest of your existing HTTP request code...
}

func createFacts(data map[string]string) []map[string]string {
    facts := make([]map[string]string, 0, len(data))
    for k, v := range data {
        facts = append(facts, map[string]string{
            "title": k,
            "value": v,
        })
    }
    return facts
}

// https://discord.com/developers/docs/resources/webhook
type discordWebhook struct {
	ThreadName string `json:"thread_name"`
	Content    string `json:"content"`
}

func sendDiscordWebhook(orig incomingWebhook) {
	webhookUrl := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookUrl == "" {
		// not configured
		return
	}

	discord := discordWebhook{
		ThreadName: orig.Message,
	}

	buf := new(bytes.Buffer)
	for key, val := range orig.Data {
		fmt.Fprintf(buf, "%s=\"%s\"\n", key, val)
	}
	discord.Content = buf.String()
	if len(discord.Content) >= 2000 {
		r := []rune(discord.Content)
		trunc := r[:1990]
		discord.Content = string(trunc) + "\n...\n"
	} else if len(discord.Content) == 0 {
		discord.Content = orig.Message
	}

	body, err := json.Marshal(discord)
	if err != nil {
		fmt.Printf("sendDiscordWebhook json.Marshall failed: %v\n", err)
		return
	}

	u, err := url.Parse(webhookUrl)
	if err != nil {
		fmt.Printf("sendDiscordWebhook url.Parse failed: %v\n", err)
		return
	}
	query := u.Query()
	query.Set("wait", "true")
	u.RawQuery = query.Encode()
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("sendDiscordWebhook http.NewRequest failed: %v\n", err)
		return
	}

	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	_, err = client.Do(req)
	if err != nil {
		fmt.Printf("sendDiscordWebhook client.Do failed: %v\n", err)
		return
	}

	return
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("TS_WEBHOOK_SECRET")
	events, err := verifyWebhookSignature(r, secret)
	if err != nil {
		fmt.Printf("[%s] handleWebhook verifyWebhookSignature: %v\n", time.Now().Format(time.RFC3339Nano), err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	fmt.Printf("[%s] handleWebhook received %d events\n", time.Now().Format(time.RFC3339Nano), len(events))
	for _, event := range events {
		sendTeamsWebhook(event)
		sendDiscordWebhook(event)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Listening for webhooks on port %s...\n", port)
	http.HandleFunc("/webhook", handleWebhook)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
