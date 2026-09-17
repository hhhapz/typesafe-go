# typesafe-go

A Go client for the [TypeSafe API](https://docs.typesafe.ai).

Requires Go 1.26 or newer.

```sh
go get github.com/hhhapz/typesafe-go
```

```go
client, err := typesafe.New(typesafe.Config{
	APIKey: "...",
})
if err != nil {
	return err
}

result, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
	State: map[string]any{
		"reference":      "Lasting for a very short time.",
		"learner_answer": "Something that doesn't last long.",
	},
	Questions: typesafe.Questions{
		"grade": typesafe.Score{
			Instructions: "How well does the learner answer match the reference?",
			Criteria:     []any{"Incorrect.", "Partially correct.", "Correct."},
		},
	},
})
if err != nil {
	return err
}

grade, _ := result.Score("grade")
```

## Retries and deadlines

By default, one call can make up to three HTTP attempts: the initial attempt and two retries.
The client retries HTTP 408, 429, and 5xx responses, connection errors, and timeouts.
After a timeout or connection failure, the server can already have processed the request.
A retry can repeat that work. The SDK does not determine whether the provider bills repeated work.

To disable retries, pass a zero retry policy:

```go
client, err := typesafe.New(
    typesafe.Config{APIKey: "..."},
    typesafe.WithRetryPolicy(typesafe.RetryPolicy{}),
)
```

`WithTimeout` applies to each attempt. The default is 10 seconds per attempt.
To bound the whole call, including retry delays, use a context deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
result, err := client.SystemOne(ctx, request)
```

Run the examples: `go test -v -run Example -args YOUR_API_KEY`.

Licensed under [MIT](LICENSE).
