package npc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelStatus describes service availability, not proof of successful inference.
// It deliberately omits local URLs, file paths, model inventories and raw errors.
type ModelStatus struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
	Ready   bool   `json:"ready"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Local) Status(ctx context.Context) ModelStatus {
	status := ModelStatus{Enabled: s.modelEnabled, Model: s.model}
	if !s.modelEnabled {
		status.Code = "disabled"
		status.Message = "Модель выключена на хосте. Перезапустите игру с OLLAMA_ENABLED=true."
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/tags", nil)
	if err != nil {
		status.Code, status.Message = "unavailable", "Не удалось проверить Ollama на хосте."
		return status
	}
	response, err := s.client.Do(req)
	if err != nil {
		status.Code, status.Message = "unavailable", "Ollama недоступна на компьютере ведущего. Откройте приложение Ollama или запустите ollama serve."
		return status
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		status.Code, status.Message = "service_error", "Ollama ответила ошибкой. Проверьте службу на компьютере ведущего."
		return status
	}
	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		status.Code, status.Message = "invalid_response", "Ollama вернула непонятный список моделей. Проверьте OLLAMA_URL на хосте."
		return status
	}
	canonical := func(name string) string {
		if !strings.Contains(name, ":") {
			return name + ":latest"
		}
		return name
	}
	for _, model := range payload.Models {
		if canonical(model.Name) == canonical(s.model) || canonical(model.Model) == canonical(s.model) {
			status.Ready, status.Code = true, "ready"
			status.Message = "Ollama доступна, выбранная модель установлена. Первая генерация может занять больше времени."
			return status
		}
	}
	status.Code = "model_missing"
	status.Message = "Выбранная модель не установлена на хосте. Выполните ollama pull " + s.model + " или укажите установленную OLLAMA_MODEL."
	return status
}

type modelError struct {
	code, message string
	cause         error
}

func (e *modelError) Error() string { return e.message }
func (e *modelError) Unwrap() error { return e.cause }

func modelFallbackReason(err error) string {
	var problem *modelError
	if errors.As(err, &problem) {
		return problem.message + " Показан справочный ответ."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Модель не успела ответить. Дождитесь загрузки Ollama на хосте и повторите вопрос. Показан справочный ответ."
	}
	return "Ответ модели не прошёл проверку доступных источников. Показан справочный ответ."
}
