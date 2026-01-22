update_grid_bin:
	git pull
	go build -o /usr/bin/grid cmd/main.go
	systemctl restart grid
