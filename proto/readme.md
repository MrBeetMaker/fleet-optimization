Make sure Go is in Path:
$env:Path += ";$(go env GOPATH)\bin"

and

$env:PYTHONPATH=".;proto"

Run this every time fleet.proto changes:
.\.venv\Scripts\python.exe -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto proto/fleet.proto proto/optim.proto; protoc -I proto --go_out=proto --go_opt=paths=source_relative --go-grpc_out=proto --go-grpc_opt=paths=source_relative proto/fleet.proto proto/optim.proto

or this:

.\.venv\Scripts\python.exe -m grpc_tools.protoc -I proto --python_out=proto --grpc_python_out=proto proto\fleet.proto proto\optim.proto

