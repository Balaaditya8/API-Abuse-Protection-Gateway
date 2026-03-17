# Intelligent API Rate Limit & Abuse Detection Gateway

A **multi-tenant API gateway** built in Go that enforces API key authentication, rate limiting, and abuse detection before forwarding requests to backend services.

The gateway protects backend APIs from excessive traffic, credential abuse, and malicious clients using **Redis-backed rate limiting and blocking mechanisms**.

---

## Architecture
Client
│
▼
API Gateway (Go + Gin)
│
├── API Key Authentication (Postgres)
├── Rate Limiting (Redis)
├── API Key Violation Tracking
├── IP Abuse Detection
│
▼
Backend Service

Supporting services:
PostgreSQL → tenants + API keys
Redis → rate limits, violations, block lists


---

## Features

- **API Key Authentication** using PostgreSQL
- **Multi-tenant rate limiting** with Redis
- **API key abuse detection** with automatic key blocking
- **IP-based protection** against repeated invalid API key attempts
- **Reverse proxy request forwarding** to backend services
- **Request logging** for observability

---

## Tech Stack

| Component | Technology |
|------|------|
Language | Go |
Framework | Gin |
Database | PostgreSQL |
Cache / Rate Limiting | Redis |
Load Testing | hey |

---

## Features

- **API Key Authentication** using PostgreSQL
- **Multi-tenant rate limiting** with Redis
- **API key abuse detection** with automatic key blocking
- **IP-based protection** against repeated invalid API key attempts
- **Reverse proxy request forwarding** to backend services
- **Request logging** for observability

---

## Tech Stack

| Component | Technology |
|------|------|
Language | Go |
Framework | Gin |
Database | PostgreSQL |
Cache / Rate Limiting | Redis |
Load Testing | hey |

---

Example results:

- **~6.7k requests/sec**
- **~28 ms median latency**
- **<40 ms p95 latency**
- **200 concurrent clients**

The gateway successfully enforces rate limits and blocks abusive clients during burst traffic.

---


