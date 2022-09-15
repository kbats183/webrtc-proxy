# syntax=docker/dockerfile:1

FROM golang:1.19-alpine

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
COPY *.go ./
COPY index.html ./

RUN go mod download

RUN go build -o /webrtc-proxy

EXPOSE 9080
EXPOSE 1935
EXPOSE 5353
EXPOSE 30000-65535

CMD [ "/webrtc-proxy" ]