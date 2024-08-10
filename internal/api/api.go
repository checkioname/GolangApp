package api 

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/checkioname/GolangApp/internal/store/pgstore"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)


type apiHandler struct {
  q *pgstore.Queries //futuramente substituir para uma abstração
  r *chi.Mux //pacote para criar routers em go
  upgrader websocket.Upgrader
  subscribers map[string]map[*websocket.Conn]context.CancelFunc
  mu *sync.Mutex //utilizar mutex para garantir que o acesso ao map seja somente um por vez
}


func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request){
  h.r.ServeHTTP(w, r)
}


func NewHandler(q *pgstore.Queries) http.Handler {
  a := apiHandler{
    q: q,
    upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool{ return true}}, //Check origin clojure -> recebe um request e retorna true ou false
    subscribers: make(map[string]map[*websocket.Conn]context.CancelFunc),
    mu : &sync.Mutex{},
  }

  r := chi.NewRouter()

  //o proprio chi fornece varios middlewares
  //adicionar middlewares (O requestID garante id nas requests,  recoverer garante que o servidor nao crashe em algum erro no sistema)
  // middleware de log
  r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)
  
  //enable cors (olhar docs do chi)
  r.Use(cors.Handler(cors.Options{
    AllowedOrigins: []string{"https://*", "http://"},
    AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
    AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
    ExposedHeaders: []string{"Link"},
    AllowCredentials: false,
    MaxAge: 300,
  }))

  r.Get("/subscribe/{rood_id}", a.handleSubscribe)
  //agrupar todar as rotas na /api
  r.Route("/api", func(r chi.Router){
    r.Route("/rooms", func(r chi.Router){
      r.Post("/", a.handleCreateRoom)
      r.Get("/", a.handleGetRooms)

      r.Route("/{room_id}/messages", func(r chi.Router){
        r.Post("/", a.handleCreateMessage)
        r.Get("/", a.handleGetRooms)

        r.Route("/{message_id}", func(r chi.Router){
          r.Get("/", a.handlerGetRoomMessage)
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

//permitir websockets  (usar pacote gorilla)
func (h apiHandler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
  rawRoomID := chi.URLParam(r, "room_id")
  roomID, err := uuid.Parse(rawRoomID)
  if err != nil {
    http.Error(w, "invalid room id", http.StatusBadRequest)
    return
  }

  _, err = h.q.GetRoom(r.Context(), roomID)
  if err != nil{
    if errors.Is(err, pgx.ErrNoRows){
      http.Error(w, "room not found", http.StatusBadRequest)
      return
    }
    http.Error(w, "something went wrong", http.StatusInternalServerError)
  }

  //Retorna uma conexao web socket
  c, err := h.upgrader.Upgrade(w,r,nil)
  if err != nil{
    slog.Warn("Falha ao atualizar a conexao", "error", err)
    http.Error(w, "failed to upgrade to web socket connection", http.StatusBadRequest)
  }

  defer c.Close()

  ctx, cancel := context.WithCancel(r.Context())

  h.mu.Lock()
  if _, ok := h.subscribers[rawRoomID]; !ok{
    h.subscribers[rawRoomID] = make(map[*websocket.Conn]context.CancelFunc)
  }
  slog.Info("new client connect", "room_id", rawRoomID, "client_id", r.RemoteAddr)
  h.subscribers[rawRoomID][c] = cancel
  h.mu.Unlock() //depois que mexer no map, dar um unlock nele

  //ficar esperando o sinal do contexto dar Done (se o cliente ou servidor cancelar a conexao recebemos nesse canal)
  <-ctx.Done()

  h.mu.Lock()
  delete(h.subscribers[rawRoomID],c) // remove conexao do pool de conexoes
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
  if err != nil{
    slog.Error("failed to insert romm", "error", err)
    http.Error(w, "something went wrong", http.StatusInternalServerError)
    return
  }

  type response struct {
    ID string `json:"id"`
  }

  data, _ := json.Marshal(response{ID: roomID.String()})
  w.Header().Set("Content-Type", "application/json")
  _,_ = w.Write(data)
}



func (h apiHandler) handleCreateMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handlerGetRoomMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleGetRooms(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {}

