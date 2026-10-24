// Copyright 2026 beetz12. Licensed under Apache-2.0.
// `messages` - read your care.com message inbox. care.com's messaging runs on
// Stream Chat (getstream.io): the /app/messages page embeds a per-user
// streamToken + streamApiKey, which we use to read conversations + message text
// directly from Stream's REST API. Hand-authored; safe across regen.

package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/other/care/internal/client"
	"github.com/mvanhorn/printing-press-library/library/other/care/internal/config"

	"github.com/spf13/cobra"
)

const careUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

var reStreamToken = regexp.MustCompile(`"streamToken"\s*:\s*"([^"]+)"`)
var reStreamKey = regexp.MustCompile(`"streamApiKey"\s*:\s*"([^"]+)"`)

type careStreamCreds struct{ APIKey, Token, UserID string }

func careHTTP() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// careFetchStreamCreds pulls the Stream token + app key from the members page,
// resolving session cookies from both the custom profile-login cache and the
// standard `auth login --chrome` / `auth login --cookies-file` stores.
func careFetchStreamCreds(ctx context.Context, flags *rootFlags, hc *http.Client) (*careStreamCreds, error) {
	var cfg *config.Config
	if flags != nil {
		cfg, _ = config.Load(flags.configPath)
	}
	ck := client.ResolveSessionCookie(cfg)
	if ck == "" {
		return nil, fmt.Errorf("no care.com session; run: care-pp-cli auth profile-login or care-pp-cli auth login --chrome")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.care.com/app/messages", nil)
	req.Header.Set("Cookie", ck)
	req.Header.Set("User-Agent", careUA)
	req.Header.Set("Accept", "text/html")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	html := string(b)
	tk := reStreamToken.FindStringSubmatch(html)
	ky := reStreamKey.FindStringSubmatch(html)
	if tk == nil || ky == nil {
		if !strings.Contains(html, `"isLoggedIn":true`) {
			return nil, fmt.Errorf("not authenticated; run: care-pp-cli auth refresh")
		}
		return nil, fmt.Errorf("could not find Stream credentials on the messages page (care.com may have changed its layout)")
	}
	creds := &careStreamCreds{APIKey: ky[1], Token: tk[1]}
	if parts := strings.Split(tk[1], "."); len(parts) == 3 {
		if pb, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var c struct {
				UserID string `json:"user_id"`
			}
			_ = json.Unmarshal(pb, &c)
			creds.UserID = c.UserID
		}
	}
	if creds.UserID == "" {
		return nil, fmt.Errorf("could not read user id from Stream token")
	}
	return creds, nil
}

// --- Stream Chat response shapes (subset) ---

type streamUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type streamMessage struct {
	ID        string     `json:"id"`
	Text      string     `json:"text"`
	User      streamUser `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	Type      string     `json:"type"`
}
type streamChannelState struct {
	Channel struct {
		CID string `json:"cid"`
	} `json:"channel"`
	Messages []streamMessage `json:"messages"`
	Members  []struct {
		User streamUser `json:"user"`
	} `json:"members"`
}

func careStreamQueryChannelsPage(ctx context.Context, hc *http.Client, creds *careStreamCreds, cidFilter string, msgLimit, limit, offset int) ([]streamChannelState, error) {
	var filter map[string]any
	if cidFilter != "" {
		filter = map[string]any{"cid": cidFilter}
	} else {
		filter = map[string]any{"members": map[string]any{"$in": []string{creds.UserID}}}
	}
	payload := map[string]any{
		"filter_conditions": filter,
		"sort":              []map[string]any{{"field": "last_message_at", "direction": -1}},
		"state":             true,
		"watch":             false,
		"presence":          false,
		"limit":             limit,
		"offset":            offset,
		"message_limit":     msgLimit,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://chat.stream-io-api.com/channels?api_key="+creds.APIKey, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", creds.Token)
	req.Header.Set("stream-auth-type", "jwt")
	req.Header.Set("X-Stream-Client", "stream-chat-javascript-client")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Stream Chat error (HTTP %d): %s", resp.StatusCode, truncate(string(rb), 160))
	}
	var out struct {
		Channels []streamChannelState `json:"channels"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, err
	}
	return out.Channels, nil
}

// careStreamQueryChannels pages through Stream's /channels endpoint (which caps
// single-request limit at 30) starting at offset until limit channels are
// collected or no more channels remain.
func careStreamQueryChannels(ctx context.Context, hc *http.Client, creds *careStreamCreds, cidFilter string, msgLimit, limit, offset int) ([]streamChannelState, error) {
	if limit < 1 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	var all []streamChannelState
	curOffset := offset
	for len(all) < limit {
		pageSize := limit - len(all)
		if pageSize > 30 {
			pageSize = 30
		}
		batch, err := careStreamQueryChannelsPage(ctx, hc, creds, cidFilter, msgLimit, pageSize, curOffset)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			break
		}
		curOffset += len(batch)
	}
	return all, nil
}

// careStreamQueryChannelMessages fetches older messages before a given message
// ID for a specific channel cid ("messaging:<id>").
func careStreamQueryChannelMessages(ctx context.Context, hc *http.Client, creds *careStreamCreds, cid, beforeMsgID string, msgLimit int) (*streamChannelState, error) {
	parts := strings.SplitN(cid, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid channel cid %q", cid)
	}
	msgQuery := map[string]any{"limit": msgLimit}
	if beforeMsgID != "" {
		msgQuery["id_lt"] = beforeMsgID
	}
	payload := map[string]any{
		"state":    true,
		"watch":    false,
		"presence": false,
		"messages": msgQuery,
	}
	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("https://chat.stream-io-api.com/channels/%s/%s/query?api_key=%s", parts[0], parts[1], creds.APIKey)
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", creds.Token)
	req.Header.Set("stream-auth-type", "jwt")
	req.Header.Set("X-Stream-Client", "stream-chat-javascript-client")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Stream Chat error (HTTP %d): %s", resp.StatusCode, truncate(string(rb), 160))
	}
	var out streamChannelState
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// otherName returns the display name of the participant who is not me.
func (s streamChannelState) otherName(myUID string) string {
	for _, m := range s.Members {
		if m.User.ID != myUID && m.User.Name != "" {
			return m.User.Name
		}
	}
	for _, m := range s.Members {
		if m.User.ID != myUID {
			return m.User.ID
		}
	}
	return "(unknown)"
}

func newCareMessagesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "messages",
		Short:       "Read your care.com message inbox (conversations with caregivers)",
		Long:        "Lists your message conversations (all caregivers you're talking to, across every job) and reads individual threads. Backed by care.com's Stream Chat messaging.",
		Example:     "  care-pp-cli messages\n  care-pp-cli messages --limit 10 --json",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCareMessagesList(cmd, flags)
		},
	}
	cmd.Flags().Int("limit", 20, "max conversations to list")
	cmd.Flags().Int("offset", 0, "number of conversations to skip for pagination")
	cmd.AddCommand(newCareMessagesReadCmd(flags))
	cmd.AddCommand(newCareMessagesReplyCmd(flags))
	return cmd
}

// otherUID returns the care.com member UUID of the participant who is not me
// (Stream user ids are care.com member UUIDs).
func (s streamChannelState) otherUID(myUID string) string {
	for _, m := range s.Members {
		if m.User.ID != myUID {
			return m.User.ID
		}
	}
	return ""
}

// careFindConversation locates a unique conversation by cid or by caregiver-name
// substring, paging across the user's channels and rejecting ambiguous matches.
func careFindConversation(ctx context.Context, hc *http.Client, creds *careStreamCreds, query string, msgLimit int) (*streamChannelState, error) {
	if strings.HasPrefix(query, "messaging:") {
		chans, err := careStreamQueryChannels(ctx, hc, creds, query, msgLimit, 1, 0)
		if err != nil {
			return nil, err
		}
		if len(chans) == 0 {
			return nil, fmt.Errorf("no conversation with cid %s", query)
		}
		return &chans[0], nil
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	var matches []streamChannelState
	offset := 0
	const pageSize = 30
	for {
		// Use message_limit=1 while scanning channels by name, then load msgLimit
		// for the uniquely matched channel if msgLimit > 1.
		batch, err := careStreamQueryChannelsPage(ctx, hc, creds, "", 1, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, ch := range batch {
			if strings.Contains(strings.ToLower(ch.otherName(creds.UserID)), needle) {
				matches = append(matches, ch)
			}
		}
		if len(batch) < pageSize {
			break
		}
		offset += len(batch)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no conversation matching %q (list them: care-pp-cli messages)", query)
	}
	if len(matches) > 1 {
		descs := make([]string, 0, len(matches))
		for _, m := range matches {
			descs = append(descs, fmt.Sprintf("%s (%s)", m.otherName(creds.UserID), m.Channel.CID))
		}
		return nil, fmt.Errorf("%q matches %d conversations: %s; specify a unique name or cid", query, len(matches), strings.Join(descs, ", "))
	}
	chosen := matches[0]
	if msgLimit > 1 {
		full, err := careStreamQueryChannels(ctx, hc, creds, chosen.Channel.CID, msgLimit, 1, 0)
		if err != nil {
			return nil, err
		}
		if len(full) > 0 {
			return &full[0], nil
		}
	}
	return &chosen, nil
}

// careResolveJobForConversation returns the care.com job id most relevant to a
// conversation (needed as the careNeedIdentifier when replying).
func careResolveJobForConversation(ctx context.Context, flags *rootFlags, participantUID, cid string) (string, error) {
	vars := map[string]any{
		"loggedInUserRole":     "SEEKER",
		"participantsMetadata": []map[string]any{{"participantId": participantUID, "conversationId": cid}},
	}
	data, err := careGraphQL(ctx, flags, careQConversationsOp, careQConversations, vars)
	if err != nil {
		return "", err
	}
	var wrap struct {
		Conv []struct {
			MostRelevantJob struct {
				Job struct {
					ID string `json:"id"`
				} `json:"job"`
			} `json:"mostRelevantJob"`
		} `json:"conversationMostRelevantCareNeeds"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return "", err
	}
	if len(wrap.Conv) == 0 || wrap.Conv[0].MostRelevantJob.Job.ID == "" {
		return "", fmt.Errorf("could not resolve a job for this conversation")
	}
	return wrap.Conv[0].MostRelevantJob.Job.ID, nil
}

func newCareMessagesReplyCmd(flags *rootFlags) *cobra.Command {
	var message, messageFile string
	var confirm bool
	cmd := &cobra.Command{
		Use:   "reply <caregiver name or cid>",
		Short: "Reply to a caregiver in your inbox (dry-run unless --confirm)",
		Long: "Sends a reply in an existing conversation via care.com's official, MODERATED send path\n" +
			"(sendCareNeedMessage) - not directly to Stream Chat. Resolves the job + recipient from the\n" +
			"conversation automatically. Dry-run by default; pass --confirm to actually send.",
		// NOTE: examples never include --confirm. This command is dry-run by
		// default (see Long); an example carrying --confirm would send for real
		// when copy-pasted or exercised by a live dogfood matrix.
		Example: "  care-pp-cli messages reply \"Jane\" --message \"Are you free to chat this week?\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			if message == "" && messageFile != "" {
				b, err := os.ReadFile(messageFile)
				if err != nil {
					return fmt.Errorf("reading --message-file: %w", err)
				}
				message = strings.TrimSpace(string(b))
			}
			if strings.TrimSpace(message) == "" {
				return usageErr(fmt.Errorf("--message or --message-file is required"))
			}
			w := cmd.OutOrStdout()
			if dryRunOK(flags) {
				if !wantsHumanTable(w, flags) {
					return printJSONFiltered(w, map[string]any{
						"action":  "would reply to " + query,
						"to":      query,
						"message": message,
						"dry_run": true,
						"sent":    false,
					}, flags)
				}
				fmt.Fprintf(w, "DRY RUN - would reply to %s. Re-run with --confirm to send.\n", query)
				return nil
			}
			to := flags.timeout
			if to <= 0 {
				to = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), to)
			defer cancel()
			hc := careHTTP()
			creds, err := careFetchStreamCreds(ctx, flags, hc)
			if err != nil {
				return err
			}
			ch, err := careFindConversation(ctx, hc, creds, query, 1)
			if err != nil {
				return err
			}
			nannyUID := ch.otherUID(creds.UserID)
			nannyName := ch.otherName(creds.UserID)
			if nannyUID == "" {
				return fmt.Errorf("could not identify the caregiver in this conversation")
			}
			jobID, err := careResolveJobForConversation(ctx, flags, nannyUID, ch.Channel.CID)
			if err != nil {
				return err
			}
			name, legacyID, err := careResolveCaregiver(ctx, flags, nannyUID)
			if err != nil {
				return err
			}
			if name == "" {
				name = nannyName
			}
			if legacyID == "" {
				return fmt.Errorf("could not resolve %s's member id", nannyName)
			}
			markContacted := func() {
				// Surface (don't swallow) a failed local write so the inbox/dedupe
				// state is known to be stale after a successful moderated reply.
				if merr := careMarkContacted(nannyUID, name); merr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: reply sent, but recording the contact locally failed: %v\n", merr)
				}
			}
			dryRun := !confirm || dryRunOK(flags)
			if !wantsHumanTable(w, flags) {
				out := map[string]any{
					"action": "reply to " + name,
					"to": name, "caregiver_id": nannyUID, "member": legacyID, "cid": ch.Channel.CID,
					"job": jobID, "message": message, "dry_run": dryRun, "sent": false,
				}
				if !dryRun {
					if err := careSendMessage(ctx, flags, jobID, legacyID, message); err != nil {
						return err
					}
					out["sent"] = true
					markContacted()
				}
				return printJSONFiltered(w, out, flags)
			}
			fmt.Fprintf(w, "To:      %s (member %s)\n", name, legacyID)
			fmt.Fprintf(w, "Job:     %s\n", jobID)
			fmt.Fprintf(w, "Message:\n%s\n\n", message)
			if dryRun {
				fmt.Fprintln(w, "DRY RUN - nothing sent. Re-run with --confirm to send (via care.com's moderated path).")
				return nil
			}
			if err := careSendMessage(ctx, flags, jobID, legacyID, message); err != nil {
				return err
			}
			fmt.Fprintf(w, "Reply sent to %s.\n", name)
			markContacted()
			return nil
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "reply text")
	cmd.Flags().StringVar(&messageFile, "message-file", "", "read reply text from a file")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually send (default is dry-run)")
	return cmd
}

func runCareMessagesList(cmd *cobra.Command, flags *rootFlags) error {
	if dryRunOK(flags) {
		if !wantsHumanTable(cmd.OutOrStdout(), flags) {
			return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
				"dry_run": true,
				"action":  "would list message conversations",
			}, flags)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "would list message conversations")
		return nil
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 {
		limit = 20
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		offset = 0
	}
	to := flags.timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), to)
	defer cancel()
	hc := careHTTP()
	creds, err := careFetchStreamCreds(ctx, flags, hc)
	if err != nil {
		return err
	}
	chans, err := careStreamQueryChannels(ctx, hc, creds, "", 1, limit, offset)
	if err != nil {
		return err
	}
	type conv struct {
		Nanny    string    `json:"nanny"`
		LastFrom string    `json:"last_from"`
		LastText string    `json:"last_message"`
		LastAt   time.Time `json:"last_at"`
		CID      string    `json:"cid"`
	}
	out := []conv{}
	for _, ch := range chans {
		c := conv{Nanny: ch.otherName(creds.UserID), CID: ch.Channel.CID}
		if n := len(ch.Messages); n > 0 {
			m := ch.Messages[n-1]
			c.LastText = m.Text
			c.LastAt = m.CreatedAt
			if m.User.ID == creds.UserID {
				c.LastFrom = "you"
			} else if m.User.ID == "care" {
				c.LastFrom = "care.com"
			} else {
				c.LastFrom = c.Nanny
			}
		}
		out = append(out, c)
	}
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), out, flags)
	}
	w := cmd.OutOrStdout()
	if len(out) == 0 {
		fmt.Fprintln(w, "No conversations.")
		return nil
	}
	fmt.Fprintf(w, "%-20s %-8s %-12s  %s\n", "CAREGIVER", "LAST", "WHEN", "MESSAGE")
	for _, c := range out {
		when := ""
		if !c.LastAt.IsZero() {
			when = c.LastAt.Local().Format("Jan 2 15:04")
		}
		fmt.Fprintf(w, "%-20s %-8s %-12s  %s\n",
			truncate(c.Nanny, 20), truncate(c.LastFrom, 8), when, truncate(strings.ReplaceAll(c.LastText, "\n", " "), 50))
	}
	fmt.Fprintf(w, "\n%d conversations. Read one: care-pp-cli messages read \"<caregiver name>\"\n", len(out))
	if len(out) >= limit {
		fmt.Fprintf(w, "More conversations may exist; pass --offset %d (or a larger --limit) to continue.\n", offset+len(out))
	}
	return nil
}

func newCareMessagesReadCmd(flags *rootFlags) *cobra.Command {
	var msgLimit int
	var beforeID string
	cmd := &cobra.Command{
		Use:         "read <caregiver name or cid>",
		Short:       "Read the message thread with one caregiver",
		Example:     "  care-pp-cli messages read example-value\n  care-pp-cli messages read messaging:!members-abc --limit 100 --before <message-id>",
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "example-value"},
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			if dryRunOK(flags) {
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"dry_run": true,
						"action":  "would read message thread for " + query,
						"query":   query,
					}, flags)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would read message thread for %s\n", query)
				return nil
			}
			if msgLimit < 1 {
				msgLimit = 100
			}
			to := flags.timeout
			if to <= 0 {
				to = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), to)
			defer cancel()
			hc := careHTTP()
			creds, err := careFetchStreamCreds(ctx, flags, hc)
			if err != nil {
				return err
			}
			ch, err := careFindConversation(ctx, hc, creds, query, msgLimit)
			if err != nil {
				return err
			}
			if strings.TrimSpace(beforeID) != "" {
				older, err := careStreamQueryChannelMessages(ctx, hc, creds, ch.Channel.CID, strings.TrimSpace(beforeID), msgLimit)
				if err != nil {
					return err
				}
				ch.Messages = older.Messages
			}
			nanny := ch.otherName(creds.UserID)
			hasMore := len(ch.Messages) >= msgLimit
			nextBefore := ""
			if hasMore && len(ch.Messages) > 0 {
				nextBefore = ch.Messages[0].ID
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				type msg struct {
					ID   string    `json:"id,omitempty"`
					From string    `json:"from"`
					At   time.Time `json:"at"`
					Text string    `json:"text"`
				}
				msgs := []msg{}
				for _, m := range ch.Messages {
					from := nanny
					if m.User.ID == creds.UserID {
						from = "you"
					} else if m.User.ID == "care" {
						from = "care.com"
					}
					msgs = append(msgs, msg{ID: m.ID, From: from, At: m.CreatedAt, Text: m.Text})
				}
				payload := map[string]any{
					"caregiver": nanny,
					"cid":       ch.Channel.CID,
					"limit":     msgLimit,
					"has_more":  hasMore,
					"messages":  msgs,
				}
				if nextBefore != "" {
					payload["next_before"] = nextBefore
				}
				return printJSONFiltered(cmd.OutOrStdout(), payload, flags)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Conversation with %s (%s)\n\n", nanny, ch.Channel.CID)
			if hasMore {
				if nextBefore != "" {
					fmt.Fprintf(w, "(Showing %d messages; pass --before %s to read older messages in this thread.)\n\n", len(ch.Messages), nextBefore)
				} else {
					fmt.Fprintf(w, "(Showing the %d most recent messages; pass a larger --limit to read older messages.)\n\n", len(ch.Messages))
				}
			}
			for _, m := range ch.Messages {
				from := nanny
				if m.User.ID == creds.UserID {
					from = "You"
				} else if m.User.ID == "care" {
					from = "care.com"
				}
				when := m.CreatedAt.Local().Format("Jan 2 15:04")
				fmt.Fprintf(w, "[%s] %s:\n%s\n\n", when, from, strings.TrimSpace(m.Text))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&msgLimit, "limit", 100, "max messages to fetch from the thread")
	cmd.Flags().StringVar(&beforeID, "before", "", "fetch messages older than this message ID (pagination)")
	return cmd
}
