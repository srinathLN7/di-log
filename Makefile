compile:
	protoc api/v1/*.proto \
			--go_out= . \
			--go_opt=paths=source_relatvie \
			--proto_path=. 

test:
	go test -race ./...