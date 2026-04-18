package handlers

import (
	"sync"
	"time"

	"flower-bot/models"
)

// StateManager управляет состояниями пользователей (FSM)
type StateManager struct {
	sessions map[int64]*models.UserSession
	mu       sync.RWMutex
}

// NewStateManager создает новый менеджер состояний
func NewStateManager() *StateManager {
	return &StateManager{
		sessions: make(map[int64]*models.UserSession),
	}
}

// GetSession получает сессию пользователя
func (sm *StateManager) GetSession(userID int64) *models.UserSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return nil
	}
	return session
}

// GetUserSession получает сессию пользователя (alias для GetSession)
func (sm *StateManager) GetUserSession(userID int64) *models.UserSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{
			State:            models.StateNone,
			AppointmentDraft: models.AppointmentDraft{},
			TempData:         make(map[string]interface{}),
			MessageIDs:       []int{},
			FullName:         "",
			Phone:            "",
			IsAdmin:          false,
			LastOrderAttempt: nil,
			CreatedAt:        time.Now(),
		}
		sm.sessions[userID] = session
	}
	return session
}

// SetSession устанавливает сессию пользователя
func (sm *StateManager) SetSession(userID int64, session *models.UserSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[userID] = session
}

// SetUserSession устанавливает сессию пользователя (alias для SetSession)
func (sm *StateManager) SetUserSession(userID int64, session *models.UserSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[userID] = session
}

// GetState получает текущее состояние пользователя
func (sm *StateManager) GetState(userID int64) models.State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return models.StateNone
	}
	return session.State
}

// SetState устанавливает состояние пользователя
func (sm *StateManager) SetState(userID int64, state models.State) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.State = state
}

// ClearSession очищает всю сессию пользователя
func (sm *StateManager) ClearSession(userID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, userID)
}

// ResetState переводит пользователя в состояние StateNone
func (sm *StateManager) ResetState(userID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.State = models.StateNone
	session.AppointmentDraft = models.AppointmentDraft{}
	session.TempData = make(map[string]interface{})
}

// AddMessageToDelete добавляет ID сообщения в список на удаление
func (sm *StateManager) AddMessageToDelete(userID int64, msgID int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.MessageIDs = append(session.MessageIDs, msgID)
}

// ClearMessagesToDelete возвращает и очищает список сообщений на удаление
func (sm *StateManager) ClearMessagesToDelete(userID int64) []int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return nil
	}

	msgIDs := session.MessageIDs
	session.MessageIDs = nil
	return msgIDs
}

// AddMessageToDelete добавляет ID сообщений в список для последующего удаления
