package main

func main() {
    foreman, _ := New("./file.yml")

    err := foreman.Start()
    if err != nil {
        panic(err)
    }
}
