package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const webhookExpiryMargin = 30 * time.Second

func (rc replyContext) useProactiveAPI() bool {
	return rc.sessionWebhook == "" ||
		(rc.sessionWebhookExpiredTime > 0 &&
			time.Now().Add(webhookExpiryMargin).UnixMilli() >= rc.sessionWebhookExpiredTime)
}

func (a *Adapter) Send(ctx context.Context, reply platform.Reply) (string, error) {
	if reply.Card != nil {
		return a.sendCard(ctx, reply)
	}
	if len(reply.Media) > 0 {
		return "", errors.New("dingtalk outbound media is not implemented yet")
	}
	text := strings.TrimSpace(reply.Text)
	if text == "" {
		return "", errors.New("dingtalk reply is empty")
	}
	rc, err := a.replyContext(reply)
	if err != nil {
		return "", err
	}
	if err := a.sendMarkdown(ctx, rc, text); err != nil {
		return "", err
	}
	return "", nil
}

func (a *Adapter) replyContext(reply platform.Reply) (replyContext, error) {
	rc, ok := reply.ReplyContext.(replyContext)
	if !ok {
		rc = replyContext{}
	}
	if rc.conversationID == "" {
		rc.conversationID = reply.ChatID
	}
	if rc.robotCode == "" {
		rc.robotCode = a.robotCode
	}
	if !rc.isGroup && strings.EqualFold(reply.ChatType, "group") {
		rc.isGroup = true
	}
	if rc.senderStaffID == "" {
		rc.senderStaffID = reply.UserID
	}
	if rc.conversationID == "" && rc.senderStaffID == "" {
		return replyContext{}, errors.New("dingtalk reply has no conversation context")
	}
	if !rc.isGroup && rc.senderStaffID == "" {
		return replyContext{}, errors.New("dingtalk direct reply has no senderStaffId")
	}
	return rc, nil
}

func (a *Adapter) sendMarkdown(ctx context.Context, rc replyContext, text string) error {
	text = truncateUTF8(text, maxCardBodyBytes)
	title := cardTitleFromContent(text)
	content := preprocessDingTalkMarkdown(text)
	if rc.useProactiveAPI() {
		return a.sendProactive(ctx, rc, title, content)
	}
	return a.sendWebhook(ctx, rc, title, content)
}

func (a *Adapter) sendWebhook(ctx context.Context, rc replyContext, title, content string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  content,
		},
	}
	body, status, err := a.requestJSON(ctx, http.MethodPost, rc.sessionWebhook, payload, "", nil)
	if err != nil {
		return fmt.Errorf("send dingtalk webhook reply: %w", err)
	}
	return validateSendResponse(status, body, false)
}

func (a *Adapter) sendProactive(ctx context.Context, rc replyContext, title, content string) error {
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return err
	}

	var path string
	var payload map[string]any
	if rc.isGroup {
		if rc.conversationID == "" {
			return errors.New("dingtalk group proactive send requires conversationId")
		}
		msgParam, _ := json.Marshal(map[string]string{"text": content})
		path = "/v1.0/robot/groupMessages/send"
		payload = map[string]any{
			"robotCode":          rc.robotCode,
			"openConversationId": rc.conversationID,
			"msgKey":             "sampleMarkdown",
			"msgParam":           string(msgParam),
		}
	} else {
		if rc.senderStaffID == "" {
			return errors.New("dingtalk direct proactive send requires senderStaffId")
		}
		msgParam, _ := json.Marshal(map[string]string{"title": title, "text": content})
		path = "/v1.0/robot/oToMessages/batchSend"
		payload = map[string]any{
			"robotCode": rc.robotCode,
			"userIds":   []string{rc.senderStaffID},
			"msgKey":    "sampleMarkdown",
			"msgParam":  string(msgParam),
		}
	}

	body, status, err := a.requestJSON(ctx, http.MethodPost, a.apiURL(path), payload, token, nil)
	if err != nil {
		return fmt.Errorf("send dingtalk proactive reply: %w", err)
	}
	return validateSendResponse(status, body, true)
}

func validateSendResponse(status int, body []byte, proactive bool) error {
	var result struct {
		ErrCode                   *int     `json:"errcode"`
		Code                      string   `json:"code"`
		ProcessQueryKey           string   `json:"processQueryKey"`
		InvalidStaffIDList        []string `json:"invalidStaffIdList"`
		FlowControlledStaffIDList []string `json:"flowControlledStaffIdList"`
	}
	decodeErr := json.Unmarshal(body, &result)
	if status != http.StatusOK {
		return fmt.Errorf("dingtalk send rejected: status=%d code=%q body=%s", status, result.Code, truncateBytes(body, 512))
	}
	if decodeErr != nil {
		return fmt.Errorf("dingtalk send response is unreadable (delivery unconfirmed): %w", decodeErr)
	}
	if result.ErrCode != nil && *result.ErrCode != 0 {
		return fmt.Errorf("dingtalk send rejected: errcode=%d", *result.ErrCode)
	}
	if result.Code != "" {
		return fmt.Errorf("dingtalk send rejected: code=%q", result.Code)
	}
	if !proactive {
		if result.ErrCode == nil {
			return errors.New("dingtalk webhook response missing errcode (delivery unconfirmed)")
		}
		return nil
	}
	if len(result.InvalidStaffIDList) > 0 || len(result.FlowControlledStaffIDList) > 0 {
		return fmt.Errorf(
			"dingtalk proactive send rejected recipients: invalid=%d rate_limited=%d",
			len(result.InvalidStaffIDList), len(result.FlowControlledStaffIDList),
		)
	}
	if strings.TrimSpace(result.ProcessQueryKey) == "" {
		return errors.New("dingtalk proactive response missing processQueryKey (delivery unconfirmed)")
	}
	return nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		max = 64 << 10
	}
	return io.ReadAll(io.LimitReader(r, max+1))
}
