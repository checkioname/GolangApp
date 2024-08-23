package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/checkioname/GolangApp/internal/core/domain"
	"github.com/checkioname/GolangApp/internal/infraestructure/db/store/pgstore"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

type ApiHandler struct {
	Q           *pgstore.Queries //futuramente substituir para uma abstração
	R           *chi.Mux         //pacote para criar routers em go
	Upgrader    websocket.Upgrader
	Subscribers map[string]map[*websocket.Conn]context.CancelFunc
	Mu          *sync.Mutex //utilizar mutex para garantir que o acesso ao map seja somente um por vez
}

func (h ApiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.R.ServeHTTP(w, r)
}

// envia resposta em formato json
func SendJson(w http.ResponseWriter, rawData any) {
	data, _ := json.Marshal(rawData)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// permitir websockets  (usar pacote gorilla)
func (h ApiHandler) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	rawRoomID := chi.URLParam(r, "room_id")
	roomID, err := uuid.Parse(rawRoomID)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	_, err = h.Q.GetRoom(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusBadRequest)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
	}

	//Retorna uma conexao web socket
	c, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("Falha ao atualizar a conexao", "error", err)
		http.Error(w, "failed to upgrade to web socket connection", http.StatusBadRequest)
	}

	defer c.Close()

	ctx, cancel := context.WithCancel(r.Context())

	h.Mu.Lock()
	if _, ok := h.Subscribers[rawRoomID]; !ok {
		h.Subscribers[rawRoomID] = make(map[*websocket.Conn]context.CancelFunc)
	}
	slog.Info("new client connect", "room_id", rawRoomID, "client_id", r.RemoteAddr)
	h.Subscribers[rawRoomID][c] = cancel
	h.Mu.Unlock() //depois que mexer no map, dar um unlock nele

	//ficar esperando o sinal do contexto dar Done (se o cliente ou servidor cancelar a conexao recebemos nesse canal)
	<-ctx.Done()

	h.Mu.Lock()
	delete(h.Subscribers[rawRoomID], c) // remove conexao do pool de conexoes
	h.Mu.Unlock()
}

func (h ApiHandler) HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var room domain.Room
	if err := json.NewDecoder(r.Body).Decode(&room); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	//inserir sala no banco de dados
	roomID, err := h.Q.InsertRoom(r.Context(), room.Theme)
	if err != nil {
		slog.Error("failed to insert romm", "error", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		ID string `json:"id"`
	}

	SendJson(w, response{ID: roomID.String()})
}

func (h ApiHandler) notifyClientes(msg domain.MessageResponse) {
	h.Mu.Lock()
	defer h.Mu.Unlock()

	subscribers, ok := h.Subscribers[msg.RoomID]
	if !ok || len(subscribers) == 0 {
		return
	}

	for conn, cancel := range subscribers {
		if err := conn.WriteJSON(msg); err != nil {
			slog.Error("failed to send mesage to client", "error", err)
			cancel()
		}
	}
}

//////////////////////////
//enviar mensagens na sala
//////////////////////////

func (h ApiHandler) HandleCreateMessage(w http.ResponseWriter, r *http.Request) {
	//pegar o id da sala para gravar a mensagem e fazer cast
	rawRoomID := chi.URLParam(r, "room_id")
	roomID, _ := uuid.Parse(rawRoomID)

	//ler a mensagem recebida na request
	var message domain.MessageRequest
	//decodar o body da request e armazenar na variavel body -> decode(body)
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		http.Error(w, "unable to decode json", http.StatusBadRequest)
		return
	}

	//se sucesso
	//gravar a mensagem no banco de dados usar a estrutura de parametros
	params := pgstore.InsertMessageParams{
		RoomID:  roomID,
		Message: message.Message,
	}

	messageID, err := h.Q.InsertMessage(r.Context(), params)
	if err != nil {
		http.Error(w, "failed to create message", http.StatusInternalServerError)
		return
	}

	//se tiver gravado no banco, enviar resposta para o cliente
	type response struct {
		ID string `json:"id"`
	}

	SendJson(w, response{ID: messageID.String()})

	//notificar os clientes em uma go routine
	go h.notifyClientes(domain.MessageResponse{
		Kind:   domain.MessageKindMessageCreated,
		RoomID: rawRoomID,
		Value: domain.MessageMessageCreated{
			ID:      messageID.String(),
			Message: message.Message,
		},
	})
}

///////////////
// retornar mensagens de uma sala
///////////////

func (h ApiHandler) HandleGetRoomMessage(w http.ResponseWriter, r *http.Request) {

	//passar  o contexto e id da sala
	_, _, roomID, _ := h.GetRoomInfo(w, r)

	messages, err := h.Q.GetRoomMessages(r.Context(), roomID)
	if err != nil {
		http.Error(w, "Interval server error", http.StatusInternalServerError)
		return
	}

	//se a lista for nula, retornar uma lista vazia
	if messages == nil {
		messages = []pgstore.GetRoomMessagesRow{}
	}

	//caso tenha lista de mensagens, retornar elas
	SendJson(w, messages)
}

////////////////
// get all rooms
////////////////

func (h ApiHandler) HandleGetRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.Q.GetRooms(r.Context())
	if err != nil {
		slog.Error("couldnt get any room", "Error", err)
		http.Error(w, "Failed to retrieve rooms", http.StatusInternalServerError)
	}

	if rooms == nil {
		rooms = []pgstore.Room{}
	}

	fmt.Println(rooms)

	SendJson(w, rooms)
}

////////////////
// reage a uma mensagem
////////////////

func (h ApiHandler) HandleReactToMessage(w http.ResponseWriter, r *http.Request) {
	// recebe o id da sala e chama o metodo de reagir
	_, rawRoomID, _, _ := h.GetRoomInfo(w, r)

	//se pegar o id da mensagem, entao reagir a uma mensagem
	rawMessageID := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageID)

	reactionCount, err := h.Q.ReactToMessage(r.Context(), messageID)

	if err != nil {
		http.Error(w, "internal server error", http.StatusBadRequest)
		return
	}

	type response struct {
		Count int64 `json:"count"`
	}

	//enviar response para o cliente com o numero de reacoes
	SendJson(w, response{Count: reactionCount})

	//notificar os clientes
	go h.notifyClientes(domain.MessageResponse{
		Kind:   domain.MessageKindMessageReactionIncreased,
		RoomID: rawRoomID,
		Value: domain.MessageMessageReactionIncreased{
			ID:    rawMessageID,
			Count: reactionCount,
		},
	})
}

// /////////////////
// apaga a reação de uma mensagem
// ////////////////
func (h ApiHandler) HandleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {
	rawMessageId := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageId)

	numberMessagesDelete, err := h.Q.RemoveReactionFromMessage(r.Context(), messageID)
	if err != nil {
		slog.Error("couldnt get any room", "Error", err)
		http.Error(w, "internal server error", http.StatusBadRequest)
		return
	}

	type response struct {
		Count int64 `json:"count"`
	}

	SendJson(w, response{Count: numberMessagesDelete})
}

///////////////////
//marca mensagem como respondida
//////////////////

func (h ApiHandler) HandleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {
	_, rawRoomID, _, _ := h.GetRoomInfo(w, r)

	rawMessageID := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageID)

	err := h.Q.MarkMessageAsAnswered(r.Context(), messageID)
	if err != nil {
		slog.Error("Could mark message as answered", "Error", err)
		http.Error(w, "internal server error", http.StatusBadRequest)
		return
	}

	var message domain.MessageRequest
	//decodar o body da request e armazenar na variavel body -> decode(body)
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		http.Error(w, "unable to decode json", http.StatusBadRequest)
		return
	}

	type response struct {
		Message string `json:"message"`
	}

	SendJson(w, response{Message: "Mensagem marcada com sucesso"})

	//notificar os clientes
	go h.notifyClientes(domain.MessageResponse{
		Kind:   domain.MessageKindMessageAnswered,
		RoomID: rawRoomID,
		Value: domain.MessageMessageAnswered{
			ID:      rawMessageID,
			Message: message.Message,
		},
	})

}

///////////////////
//retorna as informações de uma sala
//////////////////

func (h ApiHandler) GetRoomInfo(w http.ResponseWriter, r *http.Request) (pgstore.Room, string, uuid.UUID, bool) {
	//pegar o id da sala
	rawRoomID := chi.URLParam(r, "room_id")

	//decodar o id
	roomID, err := uuid.Parse(rawRoomID)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return pgstore.Room{}, "", uuid.UUID{}, false
	}

	room, err := h.Q.GetRoom(r.Context(), roomID)
	if err != nil {
		http.Error(w, "failed to get room", http.StatusInternalServerError)
		return pgstore.Room{}, "", uuid.UUID{}, false
	}

	return room, rawRoomID, roomID, true
}
