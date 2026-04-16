package handlers

import (
	"sync"

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

// SetSession устанавливает сессию пользователя
func (sm *StateManager) SetSession(userID int64, session *models.UserSession) {
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

// GetOrderDraft получает черновик заказа
func (sm *StateManager) GetOrderDraft(userID int64) *models.OrderDraft {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return nil
	}
	return &session.OrderDraft
}

// SetOrderDraft устанавливает черновик заказа
func (sm *StateManager) SetOrderDraft(userID int64, draft models.OrderDraft) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.OrderDraft = draft
}

// GetCustomDraft получает текст кастомного букета
func (sm *StateManager) GetCustomDraft(userID int64) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return ""
	}
	return session.CustomDraft
}

// SetCustomDraft устанавливает текст кастомного букета
func (sm *StateManager) SetCustomDraft(userID int64, draft string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.CustomDraft = draft
}

// GetTempData получает временные данные
func (sm *StateManager) GetTempData(userID int64) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[userID]
	if !exists {
		return ""
	}
	return session.TempData
}

// AppendTempData securely appends to temporary data
func (sm *StateManager) AppendTempData(userID int64, data string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.TempData = session.TempData + data
	return session.TempData
}

// SetTempData устанавливает временные данные
func (sm *StateManager) SetTempData(userID int64, data string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[userID]
	if !exists {
		session = &models.UserSession{}
		sm.sessions[userID] = session
	}
	session.TempData = data
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
	session.OrderDraft = models.OrderDraft{}
	session.CustomDraft = ""
	session.TempData = ""
}
