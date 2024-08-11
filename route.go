package route




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
