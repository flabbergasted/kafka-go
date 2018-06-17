package connections

import "fmt"

//ILogger represents a struct that can perform logging
type ILogger interface {
	Log(s string)
	LogError(e error)
}

//FmtLogger implements ILogger by writing to standard fmt.Print
type FmtLogger struct{}

//Log prints the s parameter to fmt.Println
func (f FmtLogger) Log(s string) {
	fmt.Println(s)
}

//LogError prints the err parameter to fmt.Printf
func (f FmtLogger) LogError(e error) {
	fmt.Printf("error: %v \n", e)
}
