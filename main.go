package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/patrickmn/go-cache"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration
	failures     int
	lastFailTime time.Time
	state        CircuitState
	nextAttempt  time.Time
	mu           sync.RWMutex
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        CircuitClosed,
	}
}

func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	if cb.state == CircuitOpen && now.After(cb.nextAttempt) {
		cb.state = CircuitHalfOpen
	}

	if cb.state == CircuitOpen {
		return fmt.Errorf("circuit breaker is open")
	}

	err := fn()

	if err != nil {
		cb.failures++
		cb.lastFailTime = now

		if cb.failures >= cb.maxFailures {
			cb.state = CircuitOpen
			cb.nextAttempt = now.Add(cb.resetTimeout)
		}
		return err
	}

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
	}
	cb.failures = 0
	return nil
}

func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == CircuitOpen
}

func (cb *CircuitBreaker) GetStatus() (CircuitState, int, time.Time) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state, cb.failures, cb.nextAttempt
}

type PaymentJob struct {
	CorrelationID string
	Amount        float64
	Processor     string
	RequestedAt   time.Time
}

type PaymentWorkerPool struct {
	jobQueue     chan PaymentJob
	batchQueue   chan []PaymentJob
	dbQueue      chan PaymentJob
	workers      int
	batchWorkers int
	dbWorkers    int
	ps           *PaymentService
}

type PaymentRequest struct {
	CorrelationID string  `json:"correlationId" binding:"required"`
	Amount        float64 `json:"amount" binding:"required"`
}

type PaymentProcessorRequest struct {
	CorrelationID string    `json:"correlationId"`
	Amount        float64   `json:"amount"`
	RequestedAt   time.Time `json:"requestedAt"`
}

type PaymentProcessorResponse struct {
	Message string `json:"message"`
}

type HealthCheckResponse struct {
	Failing bool `json:"failing"`
}

type PaymentsSummaryResponse struct {
	Default  ProcessorSummary `json:"default"`
	Fallback ProcessorSummary `json:"fallback"`
}

type ProcessorSummary struct {
	TotalRequests int64   `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

type PaymentProcessor struct {
	Name            string
	URL             string
	IsDefault       bool
	LastHealthCheck time.Time
	IsHealthy       bool
	CircuitBreaker  *CircuitBreaker
}

type PaymentService struct {
	db                *sql.DB
	cache             *cache.Cache
	defaultProcessor  *PaymentProcessor
	fallbackProcessor *PaymentProcessor
	httpClient        *http.Client
	workerPool        *PaymentWorkerPool
}

func main() {
	db, err := initDB()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	c := cache.New(5*time.Minute, 10*time.Minute)

	defaultProcessor := &PaymentProcessor{
		Name:           "default",
		URL:            os.Getenv("PAYMENT_PROCESSOR_URL_DEFAULT"),
		IsDefault:      true,
		IsHealthy:      true,
		CircuitBreaker: NewCircuitBreaker(1, 1*time.Second),
	}

	fallbackProcessor := &PaymentProcessor{
		Name:           "fallback",
		URL:            os.Getenv("PAYMENT_PROCESSOR_URL_FALLBACK"),
		IsDefault:      false,
		IsHealthy:      true,
		CircuitBreaker: NewCircuitBreaker(1, 1*time.Second),
	}

	if defaultProcessor.URL == "" {
		defaultProcessor.URL = "http://localhost:8001"
	}
	if fallbackProcessor.URL == "" {
		fallbackProcessor.URL = "http://localhost:8002"
	}

	log.Printf("Default processor URL: %s", defaultProcessor.URL)
	log.Printf("Fallback processor URL: %s", fallbackProcessor.URL)

	httpClient := &http.Client{
		Timeout: 800 * time.Millisecond,
		Transport: &http.Transport{
			MaxIdleConns:        300,
			MaxIdleConnsPerHost: 150,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
			MaxConnsPerHost:     100,
			ForceAttemptHTTP2:   true,
		},
	}

	paymentService := &PaymentService{
		db:                db,
		cache:             c,
		defaultProcessor:  defaultProcessor,
		fallbackProcessor: fallbackProcessor,
		httpClient:        httpClient,
	}

	workerPool := &PaymentWorkerPool{
		jobQueue:     make(chan PaymentJob, 5000),
		batchQueue:   make(chan []PaymentJob, 1000),
		dbQueue:      make(chan PaymentJob, 5000),
		workers:      5,
		batchWorkers: 250,
		dbWorkers:    10,
		ps:           paymentService,
	}
	paymentService.workerPool = workerPool

	workerPool.Start()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	r.POST("/payments", paymentService.handlePayment)
	r.GET("/payments-summary", paymentService.handlePaymentsSummary)
	r.POST("/purge-payments", paymentService.handlePurgePayments)

	log.Println("Starting server on port 9999...")
	r.Run(":9999")
}

func initDB() (*sql.DB, error) {
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	if dbHost == "" {
		dbHost = "postgres"
	}
	if dbPort == "" {
		dbPort = "5432"
	}
	if dbUser == "" {
		dbUser = "postgres"
	}
	if dbPassword == "" {
		dbPassword = "password"
	}
	if dbName == "" {
		dbName = "rinha"
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(40)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(10 * time.Minute)

	for i := 0; i < 30; i++ {
		if err := db.Ping(); err == nil {
			break
		}
		if i == 29 {
			return nil, fmt.Errorf("database connection timeout after 30 attempts")
		}
		log.Printf("Database connection attempt %d/30 failed, retrying...", i+1)
		time.Sleep(time.Second)
	}

	return db, nil
}

func (pool *PaymentWorkerPool) Start() {
	for i := 0; i < pool.workers; i++ {
		go pool.paymentWorker()
	}

	for i := 0; i < pool.batchWorkers; i++ {
		go pool.batchWorker()
	}

	for i := 0; i < pool.dbWorkers; i++ {
		go pool.dbWorker()
	}

	log.Printf("Started worker pool with %d payment workers, %d batch workers, %d db workers",
		pool.workers, pool.batchWorkers, pool.dbWorkers)
}

func (pool *PaymentWorkerPool) paymentWorker() {
	batchSize := 50
	batch := make([]PaymentJob, 0, batchSize)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case job := <-pool.jobQueue:
			batch = append(batch, job)

			if len(batch) >= batchSize {
				pool.batchQueue <- batch
				batch = make([]PaymentJob, 0, batchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				pool.batchQueue <- batch
				batch = make([]PaymentJob, 0, batchSize)
			}
		}
	}
}

func (pool *PaymentWorkerPool) batchWorker() {
	for batch := range pool.batchQueue {
		var wg sync.WaitGroup

		for _, job := range batch {
			wg.Add(1)
			go func(j PaymentJob) {
				defer wg.Done()

				var success bool
				var processor *PaymentProcessor
				attempt := 0

				for !success {
					attempt++

					processor = pool.ps.defaultProcessor

					err := processor.CircuitBreaker.Call(func() error {
						if pool.ps.sendToProcessorOptimized(processor, j) {
							return nil
						}
						return fmt.Errorf("default processor request failed")
					})

					if err == nil {
						success = true
						break
					}

					processor = pool.ps.fallbackProcessor

					err = processor.CircuitBreaker.Call(func() error {
						if pool.ps.sendToProcessorOptimized(processor, j) {
							return nil
						}
						return fmt.Errorf("fallback processor request failed")
					})

					if err == nil {
						success = true
						break
					}

					waitTime := time.Duration(min(attempt, 5)) * time.Second

					time.Sleep(waitTime)

				}

				j.Processor = processor.Name
				select {
				case pool.dbQueue <- j:
				default:
					log.Printf("DB queue full, payment %s may be lost", j.CorrelationID)
				}

			}(job)
		}
		wg.Wait()
	}
}

func (pool *PaymentWorkerPool) dbWorker() {
	batchSize := 10
	batch := make([]PaymentJob, 0, batchSize)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case job := <-pool.dbQueue:
			batch = append(batch, job)

			if len(batch) >= batchSize {
				pool.ps.storeBatchPayments(batch)
				batch = make([]PaymentJob, 0, batchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				pool.ps.storeBatchPayments(batch)
				batch = make([]PaymentJob, 0, batchSize)
			}
		}
	}
}

var (
	acceptedResponse    = []byte(`{"message":"Payment accepted for processing"}`)
	unavailableResponse = []byte(`{"error":"Service temporarily unavailable"}`)
)

func (ps *PaymentService) handlePayment(c *gin.Context) {
	var req PaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job := PaymentJob{
		CorrelationID: req.CorrelationID,
		Amount:        req.Amount,
		Processor:     "",
		RequestedAt:   time.Now(),
	}

	select {
	case ps.workerPool.jobQueue <- job:
		c.Writer.WriteHeader(http.StatusAccepted)
		c.Writer.Write(acceptedResponse)
	default:
		c.Writer.WriteHeader(http.StatusServiceUnavailable)
		c.Writer.Write(unavailableResponse)
	}
}

func (ps *PaymentService) sendToProcessorOptimized(processor *PaymentProcessor, job PaymentJob) bool {
	paymentReq := PaymentProcessorRequest{
		CorrelationID: job.CorrelationID,
		Amount:        job.Amount,
		RequestedAt:   job.RequestedAt,
	}

	jsonData, err := json.Marshal(paymentReq)
	if err != nil {
		return false
	}

	url := processor.URL + "/payments"

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return false
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Rinha-Token", "123")

	start := time.Now()
	resp, err := ps.httpClient.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("Processor %s timeout after %v for payment %s", processor.Name, duration, job.CorrelationID)
		}
		return false
	}
	defer resp.Body.Close()

	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	return success
}

func (ps *PaymentService) storeBatchPayments(jobs []PaymentJob) {
	if len(jobs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	values := make([]interface{}, 0, len(jobs)*3)
	placeholders := make([]string, 0, len(jobs))

	for i, job := range jobs {
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d)", i*3+1, i*3+2, i*3+3))
		values = append(values, job.CorrelationID, job.Amount, job.Processor)
	}

	query := fmt.Sprintf(`
		INSERT INTO payments (correlation_id, amount, processor) 
		VALUES %s `,
		strings.Join(placeholders, ","))

	_, err := ps.db.ExecContext(ctx, query, values...)
	if err != nil {
		log.Printf("Failed to store batch payments: %v", err)
	}
}

func (ps *PaymentService) handlePaymentsSummary(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")

	var query string
	var args []interface{}

	if from != "" && to != "" {
		query = `
			SELECT processor, COUNT(*), COALESCE(SUM(amount), 0)
			FROM payments 
			WHERE processed_at >= $1 AND processed_at <= $2
			GROUP BY processor
		`
		args = []interface{}{from, to}
	} else if from != "" {
		query = `
			SELECT processor, COUNT(*), COALESCE(SUM(amount), 0)
			FROM payments 
			WHERE processed_at >= $1
			GROUP BY processor
		`
		args = []interface{}{from}
	} else if to != "" {
		query = `
			SELECT processor, COUNT(*), COALESCE(SUM(amount), 0)
			FROM payments 
			WHERE processed_at <= $1
			GROUP BY processor
		`
		args = []interface{}{to}
	} else {
		query = `
			SELECT processor, COUNT(*), COALESCE(SUM(amount), 0)
			FROM payments 
			GROUP BY processor
		`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := ps.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Database error in payments summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	defer rows.Close()

	summary := PaymentsSummaryResponse{
		Default:  ProcessorSummary{TotalRequests: 0, TotalAmount: 0},
		Fallback: ProcessorSummary{TotalRequests: 0, TotalAmount: 0},
	}

	for rows.Next() {
		var processor string
		var count int64
		var amount float64

		if err := rows.Scan(&processor, &count, &amount); err != nil {
			continue
		}

		if processor == "default" {
			summary.Default.TotalRequests = count
			summary.Default.TotalAmount = amount
		} else if processor == "fallback" {
			summary.Fallback.TotalRequests = count
			summary.Fallback.TotalAmount = amount
		}
	}

	c.JSON(http.StatusOK, summary)
}

func (ps *PaymentService) handlePurgePayments(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ps.db.ExecContext(ctx, "DELETE FROM payments")
	if err != nil {
		log.Printf("Failed to purge payments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to purge payments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Payments purged successfully"})
}
