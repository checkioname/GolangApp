package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/checkioname/GolangApp/internal/infraestructure/db/store/pgstore"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

type apiHandler struct {
	q           *pgstore.Queries //futuramente substituir para uma abstração
	r           *chi.Mux         //pacote para criar routers em go
	upgrader    websocket.Upgrader
	subscribers map[string]map[*websocket.Conn]context.CancelFunc
	mu          *sync.Mutex //utilizar mutex para garantir que o acesso ao map seja somente um por vez
}

// instancia um api handler e define as rotas da nossa api
func NewHandler(q *pgstore.Queries) http.Handler {
	a := apiHandler{
		q:           q,
		upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}, //Check origin clojure -> recebe um request e retorna true ou false
		subscribers: make(map[string]map[*websocket.Conn]context.CancelFunc),
		mu:          &sync.Mutex{},
	}

	r := chi.NewRouter()

	//o proprio chi fornece varios middlewares
	//adicionar middlewares (O requestID garante id nas requests,  recoverer garante que o servidor nao crashe em algum erro no sistema)
	// middleware de log
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)

	//enable cors (olhar docs do chi)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/subscribe/{room_id}", a.handleSubscribe)
	//agrupar todar as rotas na /api
	r.Route("/api", func(r chi.Router) {
		r.Route("/rooms", func(r chi.Router) {
			r.Post("/", a.handleCreateRoom)
			r.Get("/", a.handleGetRooms)

			r.Route("/{room_id}/messages", func(r chi.Router) {
				r.Post("/", a.handleCreateMessage)
				r.Get("/", a.handleGetRooms)

				r.Route("/{message_id}", func(r chi.Router) {
					r.Get("/", a.handleGetRoomMessage)
					r.Patch("/react", a.handleReactToMessage)
					r.Delete("/react", a.handleRemoveReactFromMessage)
					r.Patch("/answer", a.handleMarkMessageAsAnswered)
				})
			})
		})
	})
	a.r = r
	return a
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

// envia resposta em formato json
func SendJson(w http.ResponseWriter, rawData any) {
	data, _ := json.Marshal(rawData)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// permitir websockets  (usar pacote gorilla)
func (h apiHandler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	rawRoomID := chi.URLParam(r, "room_id")
	roomID, err := uuid.Parse(rawRoomID)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	_, err = h.q.GetRoom(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusBadRequest)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
	}

	//Retorna uma conexao web socket
	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("Falha ao atualizar a conexao", "error", err)
		http.Error(w, "failed to upgrade to web socket connection", http.StatusBadRequest)
	}

	defer c.Close()

	ctx, cancel := context.WithCancel(r.Context())

	h.mu.Lock()
	if _, ok := h.subscribers[rawRoomID]; !ok {
		h.subscribers[rawRoomID] = make(map[*websocket.Conn]context.CancelFunc)
	}
	slog.Info("new client connect", "room_id", rawRoomID, "client_id", r.RemoteAddr)
	h.subscribers[rawRoomID][c] = cancel
	h.mu.Unlock() //depois que mexer no map, dar um unlock nele

	//ficar esperando o sinal do contexto dar Done (se o cliente ou servidor cancelar a conexao recebemos nesse canal)
	<-ctx.Done()

	h.mu.Lock()
	delete(h.subscribers[rawRoomID], c) // remove conexao do pool de conexoes
	h.mu.Unlock()
}

func (h apiHandler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	type _body struct {
		Theme string `json:"theme"`
	}

	var body _body
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	//inserir sala no banco de dados
	roomID, err := h.q.InsertRoom(r.Context(), body.Theme)
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

func (h apiHandler) notifyClientes(msg Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subscribers, ok := h.subscribers[msg.RoomID]
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

func (h apiHandler) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	//pegar o id da sala para gravar a mensagem e fazer cast
	rawRoomID := chi.URLParam(r, "room_id")
	roomID, _ := uuid.Parse(rawRoomID)

	//ler a mensagem recebida na request
	type _body struct {
		Message string `json:"message"`
	}

	var body _body
	//decodar o body da request e armazenar na variavel body -> decode(body)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "unable to decode json", http.StatusBadRequest)
		return
	}

	//se sucesso
	//gravar a mensagem no banco de dados usar a estrutura de parametros
	params := pgstore.InsertMessageParams{
		RoomID:  roomID,
		Message: body.Message,
	}

	messageID, err := h.q.InsertMessage(r.Context(), params)
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
	go h.notifyClientes(Message{
		Kind:   MessageKindMessageCreated,
		RoomID: rawRoomID,
		Value: MessageMessageCreated{
			ID:      messageID.String(),
			Message: body.Message,
		},
	})
}

///////////////
// retornar mensagens de uma sala
///////////////

func (h apiHandler) handleGetRoomMessage(w http.ResponseWriter, r *http.Request) {

	//passar  o contexto e id da sala
	_, _, roomID, _ := h.getRoomInfo(w, r)

	messages, err := h.q.GetRoomMessages(r.Context(), roomID)
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

func (h apiHandler) handleGetRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.q.GetRooms(r.Context())
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

func (h apiHandler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {
	// recebe o id da sala e chama o metodo de reagir
	_, rawRoomID, _, _ := h.getRoomInfo(w, r)

	//se pegar o id da mensagem, entao reagir a uma mensagem
	rawMessageID := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageID)

	reactionCount, err := h.q.ReactToMessage(r.Context(), messageID)

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
	go h.notifyClientes(Message{
		Kind:   MessageKindMessageReactionIncreased,
		RoomID: rawRoomID,
		Value: MessageMessageReactionIncreased{
			ID:    rawMessageID,
			Count: reactionCount,
		},
	})
}

// /////////////////
// apaga a reação de uma mensagem
// ////////////////
func (h apiHandler) handleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {
	rawMessageId := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageId)

	numberMessagesDelete, err := h.q.RemoveReactionFromMessage(r.Context(), messageID)
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

func (h apiHandler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {
	_, rawRoomID, _, _ := h.getRoomInfo(w, r)

	rawMessageID := chi.URLParam(r, "message_id")
	messageID, _ := uuid.Parse(rawMessageID)

	err := h.q.MarkMessageAsAnswered(r.Context(), messageID)
	if err != nil {
		slog.Error("Could mark message as answered", "Error", err)
		http.Error(w, "internal server error", http.StatusBadRequest)
		return
	}

	//ler a mensagem recebida na request
	type _body struct {
		Message string `json:"message"`
	}

	var body _body
	//decodar o body da request e armazenar na variavel body -> decode(body)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "unable to decode json", http.StatusBadRequest)
		return
	}

	type response struct {
		Message string `json:"message"`
	}

	SendJson(w, response{Message: "Mensagem marcada com sucesso"})

	//notificar os clientes
	go h.notifyClientes(Message{
		Kind:   MessageKindMessageAnswered,
		RoomID: rawRoomID,
		Value: MessageMessageAnswered{
			ID:      rawMessageID,
			Message: body.Message,
		},
	})

}

///////////////////
//retorna as informações de uma sala
//////////////////

func (h apiHandler) getRoomInfo(w http.ResponseWriter, r *http.Request) (pgstore.Room, string, uuid.UUID, bool) {
	//pegar o id da sala
	rawRoomID := chi.URLParam(r, "room_id")

	//decodar o id
	roomID, err := uuid.Parse(rawRoomID)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return pgstore.Room{}, "", uuid.UUID{}, false
	}

	room, err := h.q.GetRoom(r.Context(), roomID)
	if err != nil {
		http.Error(w, "failed to get room", http.StatusInternalServerError)
		return pgstore.Room{}, "", uuid.UUID{}, false
	}

	return room, rawRoomID, roomID, true

}
