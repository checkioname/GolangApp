package api 

type apiHandler struct {
  q *pgstore.Queries, //futuramente substituir para uma abstração
  r *chi.Mux //pacote para criar routers em go
}


func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request){
  h.r.ServeHTTP(w, r)
}


func NewHandler(q *pgstore.Queries) http.Handler {
  a := apiHandler{
    q: q,
  }

  r := chi.NewRouter()

  //o proprio chi fornece varios middlewares
  //adicionar middlewares (O requestID garante id nas requests,  recoverer garante que o servidor nao crashe em algum erro no sistema)
  // middleware de log
  r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)

  //agrupar todar as rotas na /api
  r.Route("/api", func(r chi.Router){
    r.Route("/rooms", func(r chi.Router){
      r.Post("/", h htpp.HandlerCreateRoom)
      r.Get("/", a.handleGetRooms)

      r.Route("/{room_id}/messages", func(r chi.Router){
        r.Post("/", a.handleCreateMessage)
        r.Get("/"bin/

        r.Route("/{message_id}", func(r chi.Router){
          r.Get("/", a.handlerGetRoomMessage)
          r.Patch("/react", a.handleReactToMessage)
          r.Delete("/react", a.handleRemoveReactFromMessage)
          r.Patch("/answer", a.handleMarkMessageAsAnswered)
        })
      })
    })

  a.r = r
  return a
} 

func (h apiHandler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleCreateMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handlerGetRoomMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleGetRooms(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {}
func (h apiHandler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {}
}
