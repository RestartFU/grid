update_grid_node_bin:
	git pull
	go build -o /usr/bin/grid-node cmd/main.go
	systemctl restart grid-node

openapi:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.3.0 --config openapi/oapi-codegen.yaml openapi/grid.yaml

run:
	go run ./cmd/main.go
