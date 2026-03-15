package deepseek

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"ds2api/internal/auth"
	"ds2api/internal/config"
)

// DeleteSessionResult 删除会话结果
type DeleteSessionResult struct {
	SessionID    string // 会话 ID
	Success      bool   // 是否成功
	ErrorMessage string // 错误信息
}

// DeleteSession 删除单个会话
func (c *Client) DeleteSession(ctx context.Context, a *auth.RequestAuth, sessionID string, maxAttempts int) (*DeleteSessionResult, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}

	result := &DeleteSessionResult{
		SessionID: sessionID,
	}

	if sessionID == "" {
		result.ErrorMessage = "session_id is required"
		return result, errors.New(result.ErrorMessage)
	}

	attempts := 0
	refreshed := false

	for attempts < maxAttempts {
		headers := c.authHeaders(a.DeepSeekToken)

		payload := map[string]any{
			"chat_session_id": sessionID,
		}

		resp, status, err := c.postJSONWithStatus(ctx, c.regular, DeepSeekDeleteSessionURL, headers, payload)
		if err != nil {
			config.Logger.Warn("[delete_session] request error", "error", err, "session_id", sessionID)
			attempts++
			continue
		}

		code := intFrom(resp["code"])
		if status == http.StatusOK && code == 0 {
			result.Success = true
			return result, nil
		}

		msg, _ := resp["msg"].(string)
		result.ErrorMessage = fmt.Sprintf("status=%d, code=%d, msg=%s", status, code, msg)
		config.Logger.Warn("[delete_session] failed", "status", status, "code", code, "msg", msg, "session_id", sessionID)

		if a.UseConfigToken {
			if isTokenInvalid(status, code, msg) && !refreshed {
				if c.Auth.RefreshToken(ctx, a) {
					refreshed = true
					continue
				}
			}
			if c.Auth.SwitchAccount(ctx, a) {
				refreshed = false
				attempts++
				continue
			}
		}
		attempts++
	}

	result.Success = false
	result.ErrorMessage = "delete session failed after retries"
	return result, errors.New(result.ErrorMessage)
}

// DeleteSessionForToken 直接使用 token 删除会话（直通模式）
func (c *Client) DeleteSessionForToken(ctx context.Context, token string, sessionID string) (*DeleteSessionResult, error) {
	result := &DeleteSessionResult{
		SessionID: sessionID,
	}

	if sessionID == "" {
		result.ErrorMessage = "session_id is required"
		return result, errors.New(result.ErrorMessage)
	}

	headers := c.authHeaders(token)
	payload := map[string]any{
		"chat_session_id": sessionID,
	}

	resp, status, err := c.postJSONWithStatus(ctx, c.regular, DeepSeekDeleteSessionURL, headers, payload)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}

	code := intFrom(resp["code"])
	if status != http.StatusOK || code != 0 {
		msg, _ := resp["msg"].(string)
		result.ErrorMessage = fmt.Sprintf("request failed: status=%d, code=%d, msg=%s", status, code, msg)
		return result, fmt.Errorf(result.ErrorMessage)
	}

	result.Success = true
	return result, nil
}

// DeleteAllSessions 删除所有会话（谨慎使用）
func (c *Client) DeleteAllSessions(ctx context.Context, a *auth.RequestAuth) (int, error) {
	const maxNoProgress = 3 // 最大无进度次数

	deleted := 0
	cursor := ""
	noProgressCount := 0

	for {
		sessions, hasMore, err := c.FetchSessionPage(ctx, a, cursor)
		if err != nil {
			return deleted, err
		}

		deletedThisRound := 0
		for _, session := range sessions {
			_, err := c.DeleteSession(ctx, a, session.ID, 1)
			if err == nil {
				deleted++
				deletedThisRound++
			}
		}

		// 无进度检测：如果连续多轮没有成功删除任何会话，退出循环
		if deletedThisRound == 0 {
			noProgressCount++
			if noProgressCount >= maxNoProgress {
				config.Logger.Warn("[delete_all_sessions] exiting due to no progress", "deleted", deleted)
				break
			}
		} else {
			noProgressCount = 0
		}

		if !hasMore || len(sessions) == 0 {
			break
		}
	}

	return deleted, nil
}

// DeleteAllSessionsForToken 直接使用 token 删除所有会话（直通模式）
func (c *Client) DeleteAllSessionsForToken(ctx context.Context, token string) (int, error) {
	const maxNoProgress = 3 // 最大无进度次数

	deleted := 0
	cursor := ""
	noProgressCount := 0

	for {
		// 获取会话列表
		headers := c.authHeaders(token)
		params := url.Values{}
		params.Set("lte_cursor.pinned", "false")
		if cursor != "" {
			params.Set("lte_cursor", cursor)
		}
		reqURL := DeepSeekFetchSessionURL + "?" + params.Encode()

		resp, status, err := c.getJSONWithStatus(ctx, c.regular, reqURL, headers)
		if err != nil {
			return deleted, err
		}

		code := intFrom(resp["code"])
		if status != http.StatusOK || code != 0 {
			msg, _ := resp["msg"].(string)
			return deleted, fmt.Errorf("fetch sessions failed: status=%d, code=%d, msg=%s", status, code, msg)
		}

		data, _ := resp["data"].(map[string]any)
		bizData, _ := data["biz_data"].(map[string]any)
		chatSessions, _ := bizData["chat_sessions"].([]any)
		hasMore, _ := bizData["has_more"].(bool)

		// 删除每个会话
		deletedThisRound := 0
		for _, s := range chatSessions {
			if m, ok := s.(map[string]any); ok {
				sessionID := stringFromMap(m, "id")
				if sessionID == "" {
					continue
				}
				_, err := c.DeleteSessionForToken(ctx, token, sessionID)
				if err == nil {
					deleted++
					deletedThisRound++
				}
			}
		}

		// 无进度检测：如果连续多轮没有成功删除任何会话，退出循环
		if deletedThisRound == 0 {
			noProgressCount++
			if noProgressCount >= maxNoProgress {
				config.Logger.Warn("[delete_all_sessions_for_token] exiting due to no progress", "deleted", deleted)
				break
			}
		} else {
			noProgressCount = 0
		}

		if !hasMore || len(chatSessions) == 0 {
			break
		}

		// 推进 cursor：从最后一个会话中提取 cursor（通常基于 updated_at 或 id）
		if len(chatSessions) > 0 {
			if lastSession, ok := chatSessions[len(chatSessions)-1].(map[string]any); ok {
				// 尝试从响应中获取 cursor 字段，或使用最后会话的 ID/updated_at
				if nextCursor, ok := bizData["cursor"].(string); ok && nextCursor != "" {
					cursor = nextCursor
				} else if nextCursor := stringFromMap(lastSession, "id"); nextCursor != "" {
					cursor = nextCursor
				}
			}
		}
	}

	return deleted, nil
}
