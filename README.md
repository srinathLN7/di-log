# di-log
A distributed commit log service built from scratch in golang. The goal of this project is to build a distributed logging system that can handle high volumes of log data across multiple nodes. The system ensures fault tolerance, scalability, and efficient log processing. It employs various distributed systems concepts and techniques to achieve these goals. Refer [here](https://github.com/srinathLN7/di-log/blob/main/internal/log/README.md) for the log design.

## Features

- **Distributed Architecture**: The logging system is designed to operate in a distributed manner, with multiple nodes handling log data and co-ordinating with each other.
- **Fault Tolerance**: The system incorporates fault tolerance mechanisms to ensure the availability and resilience of log data in the face of failures.
- **Scalability**: It is built to scale horizontally, allowing for the addition of more nodes to handle increased log data volume and traffic.
- **Log Processing**: The system provides efficient log processing capabilities, allowing for real-time analysis, filtering, and aggregation of log data.
- **High Performance**: It aims to achieve high performance by leveraging the concurrency features of Go and optimizing resource utilization.

## Getting Started

### Prerequisites

- Go programming language (version 1.13 or higher)
- A suitable IDE (VS code recommended) 

### Installation

1. Clone the repository:

   ```shell
   git clone https://github.com/srinathLN7/di-log.git
   ```

2. Change to the project directory:

   ```shell
   cd cmd
   ```

3. Build the project:

   ```shell
   go run main.go
   ```

4. Access the system at `http://localhost:8080` (or the appropriate URL).

## Contributing

Contributions to this project are welcome. If you find any issues or have suggestions for improvements, please feel free to open an issue or submit a pull request.


## References

- Book: ["Distributed Services with Go" by Travis Jeffery](https://pragprog.com/titles/tjgo/distributed-services-with-go/)
- Go documentation: https://golang.org/doc/

## License

This project is licensed under the [MIT License](LICENSE).


## Contact

For questions or further information about this project, please contact:

Email: srinath0393@gmail.com 
LinkedIn: [L. Nandakumar](https://www.linkedin.com/in/lnandakumar/)


