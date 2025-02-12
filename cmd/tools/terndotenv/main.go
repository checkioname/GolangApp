package main

import (
  "os/exec"
  "github.com/joho/godotenv"
  "fmt"
)


func main(){
  if err := godotenv.Load(); err != nil {
    panic(err)
  }
  
  cmd := exec.Command(
    "tern",
    "migrate",
    "--migrations",
    "./internal/store/pgstore/migrations",
    "--config",
    "./internal/store/pgstore/migrations/tern.conf",
   )

  output, err := cmd.CombinedOutput()
  if err != nil {
        fmt.Printf("Command execution failed with error: %v\n", err)
        fmt.Printf("Command output: %s\n", string(output))
        panic(err)
  }
  fmt.Printf("Command output: %s\n", string(output))
}
