package domain

type Message struct {
	Kind   string `json:"kind"`
	Value  any    `json:"value"`
	RoomID string `json:"-"`
}

// estruturas para cada tipo de acao/notificacao
type MessageMessageReactionIncreased struct {
	ID    string `json:"id"`
	Count int64  `json:"count"`
}

type MessageMessageCreated struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type MessageMessageAnswered struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// constantes que serao usadas para notificar os usuarios
const (
	MessageKindMessageCreated           = "message_created"
	MessageKindMessageReactionIncreased = "message_reaction_increased"
	MessageKindMessageAnswered          = "message_answered"
)
