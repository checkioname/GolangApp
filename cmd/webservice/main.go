package main

import (
  "context"
  "os"
  "net/http"
  "os/signal"
  "github.com/checkioname/GolangApp/internal/store/pgstore"
  "github.com/checkioname/GolangApp/internal/api"
)


func main(){
  if err := godotenv.Load; err != nil{
    panic(err)
  }
  
  ctx := context.Background()

  // criar um pool de conexoes com o banco de dados
  // pgx gerencia varias conexoes com o banco
  pool, err := pgxpool.New(ctx, fmt.Sprintf(
    "user=%s password=%s host%s port=%s dbname=%s", 
    os.Getenv("WS_DATABASE_USER"), 
    os.Getenv("WS_DATABASE_PASSWORD"), 
    os.Getenv("WS_DATABASE_HOST"), 
    os.Getenv("WS_DATABASE_PORT"), 
    os.Getenv("WS_DATABASE_NAME"),
  ))

  if err != nil{
    panic(err)
  }

  defer pool.Close()

  if err := poll.Ping(ctx); err != nil{
    panic(err)
  }

  
  //handle eh igual a o handler e passamos o pgstore que recebe o nosso pool de conexoes
  handler := api.NewHandler(pgstore.New(pool))

  go func(){
    if err := http.ListenAndServe(":808", handler); err != nil{
      if err != errors.Is(err, http.ErrServerClosed){
        panic(err)
      }
    }
  }()

  quit := make(chan os.Signal, 1)
  signal.Notify(quit, os.Interrupt) 
  <- quit
}
