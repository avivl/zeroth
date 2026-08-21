Here is the plan:

```json
{"effects":[{"op":"modify","target":"README.md","diff":"+Version: 2"},{"op":"modify","target":"greet.go","diff":"+func Greet(name string) string { return \"hello, \" + name }"},{"op":"modify","target":"main.go","diff":"+fmt.Println(greet.Greet(\"zeroth\"))"}]}
```
