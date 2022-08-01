package main

func main() {
    foreman, err := New("./Procfile")
    if err != nil {
        panic(err)
    }

    err = foreman.Start()
    if err != nil {
        panic(err)
    }
}
