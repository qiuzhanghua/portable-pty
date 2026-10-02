module github.com/qiuzhanghua/portable-pty

go 1.24.0

require (
	// v1.7.0 and later require go >= 1.25; v1.6.4 is the newest that does not.
	go.bug.st/serial v1.6.4
	golang.org/x/sys v0.41.0
)

require github.com/creack/goselect v0.1.2 // indirect
