update_grid_bin:
	go build -o /usr/bin/grid cmd/main.go
	sudo systemctl restart grid
