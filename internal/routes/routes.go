package routes

import (
	"net/http"

	"github.com/checkioname/GolangApp/internal/api"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func NewHandler(a *api.ApiHandler) http.Handler {

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

	r.Get("/subscribe/{room_id}", a.HandleSubscribe)
	//agrupar todar as rotas na /api
	r.Route("/api", func(r chi.Router) {
		r.Route("/rooms", func(r chi.Router) {
			r.Post("/", a.HandleCreateRoom)
			r.Get("/", a.HandleGetRooms)

			r.Route("/{room_id}/messages", func(r chi.Router) {
				r.Post("/", a.HandleCreateMessage)
				r.Get("/", a.HandleGetRooms)

				r.Route("/{message_id}", func(r chi.Router) {
					r.Get("/", a.HandleGetRoomMessage)
					r.Patch("/react", a.HandleReactToMessage)
					r.Delete("/react", a.HandleRemoveReactFromMessage)
					r.Patch("/answer", a.HandleMarkMessageAsAnswered)
				})
			})
		})
	})
	a.R = r
	return a
}
