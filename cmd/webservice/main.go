package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/checkioname/GolangApp/internal/api"
	"github.com/checkioname/GolangApp/internal/infraestructure/db/store/pgstore"
	"github.com/checkioname/GolangApp/internal/routes"
	"github.com/gorilla/websocket"

	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	ctx := context.Background()

	// criar um pool de conexoes com o banco de dados
	// pgx gerencia varias conexoes com o banco
	pool, err := pgxpool.New(ctx, fmt.Sprintf(
		"user=%s password=%s host=%s port=%s dbname=%s",
		os.Getenv("WS_DATABASE_USER"),
		os.Getenv("WS_DATABASE_PASSWORD"),
		os.Getenv("WS_DATABASE_HOST"),
		os.Getenv("WS_DATABASE_PORT"),
		os.Getenv("WS_DATABASE_NAME"),
	))

	if err != nil {
		panic(err)
	}

	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		panic(err)
	}

	apiHandler := api.ApiHandler{
		Q:           pgstore.New(pool),
		Upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}, //Check origin clojure -> recebe um request e retorna true ou false
		Subscribers: make(map[string]map[*websocket.Conn]context.CancelFunc),
		Mu:          &sync.Mutex{},
	}
	//handle eh igual a o handler e passamos o pgstore que recebe o nosso pool de conexoes
	handler := routes.NewHandler(&apiHandler)

	go func() {
		if err := http.ListenAndServe(":8080", handler); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				panic(err)
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
