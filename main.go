package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Структура DownloadTask
type DownloadTask struct {
	URL      string
	FileName string
	Size     int64
}

// Структура DownloadResult
type DownloadResult struct {
	Task         DownloadTask
	WorkerID     int
	Success      bool
	Error        error
	Duration     time.Duration
	ActualSize   int64
	DownloadedAt time.Time
}

// Структура Downloader
type Downloader struct {
	mu          sync.Mutex
	results     []DownloadResult
	client      *http.Client
	workers     int
	downloadDir string
}

// метод newDownloader для создання нових загрузчиків
func NewDownloader(workers int) *Downloader {
	return &Downloader{
		client:      &http.Client{Timeout: 15 * time.Second},
		workers:     workers,
		results:     []DownloadResult{},
		downloadDir: "downloads",
	}
}

// метод worker потрібен для роботи воркерів
func (d *Downloader) worker(workerID int, taskChan <-chan DownloadTask, resultChan chan<- DownloadResult, wg *sync.WaitGroup) {
	defer wg.Done()
	// цикл для обробки всіх завдань, які находяться у каналі
	for task := range taskChan {
		time.Sleep(time.Second * 2)
		fmt.Printf("⬇️ Worker %d: %s \n", workerID+1, task.FileName)
		res := d.downloadFile(task, workerID)
		resultChan <- res
		if res.Success {
			fmt.Printf("✅ [Worker %d] %s (%.1fKB, %.1fs)\n", workerID+1, task.FileName, float64(res.ActualSize/1024), res.Duration.Seconds())
		} else {
			fmt.Printf("❌ [Worker %d] %s (%v)\n", workerID+1, task.FileName, res.Error)
		}
	}
}

// метод downloadFile який завантажує файли з url-адресів
func (d *Downloader) downloadFile(task DownloadTask, workerID int) DownloadResult {
	// початок вімірювання часу завантаження
	start := time.Now()

	// http запит для завантаження файлу
	resp, err := d.client.Get(task.URL)
	if err != nil {
		return DownloadResult{Task: task, WorkerID: workerID, Success: false, Error: err, Duration: time.Since(start)}
	}
	// закриття респонсу
	defer resp.Body.Close()

	// перевірка на статус
	if resp.StatusCode != http.StatusOK {
		return DownloadResult{Task: task, WorkerID: workerID, Success: false, Error: fmt.Errorf("Bad status: %s", resp.Status), Duration: time.Since(start)}
	}

	// створення директорії якщо її немає
	os.MkdirAll("downloads", 0755)
	// формуємо шлях до файла і створюємо його
	outputPath := filepath.Join(d.downloadDir, task.FileName)
	file, err := os.Create(outputPath)
	if err != nil {
		return DownloadResult{Task: task, WorkerID: workerID, Success: false, Error: err, Duration: time.Since(start)}
	}
	defer file.Close()

	// копіювання тіла http-відповіді у файл
	writer, err := io.Copy(file, resp.Body)
	if err != nil {
		return DownloadResult{Task: task, WorkerID: workerID, Success: false, Error: err, Duration: time.Since(start)}
	}

	// формуємо результату виконання завдання
	return DownloadResult{
		Task:         task,
		WorkerID:     workerID,
		Success:      true,
		Duration:     time.Since(start),
		ActualSize:   writer,
		DownloadedAt: time.Now(),
	}
}

// метод додавання результату завантаження до списку Downloader.results
func (d *Downloader) addResult(result DownloadResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.results = append(d.results, result)
}

// метод приймає список завдань і роспріділяє між воркерами
func (d *Downloader) DownloadFiles(tasks []DownloadTask) {
	// вімірює загальний час завантаження
	start := time.Now()

	// канал для передачи завдань воркерам
	taskChan := make(chan DownloadTask)
	// канал для збору резільтатів від воркерів
	resultChan := make(chan DownloadResult)

	fmt.Printf("📋 Завантажень: %d | Воркерів: %d\n\n", len(tasks), d.workers)
	var wg sync.WaitGroup
	// створення воркерів
	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go d.worker(i, taskChan, resultChan, &wg)
	}

	fmt.Printf("🚀%d Worker запущені\n", d.workers)

	// горутина для відправки всіх завдань у канал
	go func() {
		for _, task := range tasks {
			taskChan <- task
		}
		close(taskChan)
	}()

	// горутина для закриття resultChan після завершення всіх воркерів
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// лічильники статистики
	succesCount := 0
	errorCount := 0
	totalSize := int64(0)
	workerStats := make(map[int]int)

	// читання результату з каналу
	for res := range resultChan {
		d.addResult(res) // додаємо резільтат
		if res.Success {
			succesCount++
			totalSize += res.ActualSize
			workerStats[res.WorkerID]++ // рахування скільки файлів зробив воркер
		} else {
			errorCount++
		}
	}
	// вивід фінальної статистики
	fmt.Println("\n🏁 Всі воркери завершили роботу")
	fmt.Println("📊 СТАТИСТИКА: ")
	fmt.Printf("✅ Успішно: %d | ❌ Помилки: %d\n", succesCount, errorCount)
	fmt.Printf("📦 Розмір: %.1fKB | ⚡ Швидкість: %.1fKB/s\n", float64(totalSize)/1024, (float64(totalSize)/1024)/time.Since(start).Seconds())

	// вивід статистики по кожному воркеру
	fmt.Print("👷 ")
	for w, f := range workerStats {
		fmt.Printf("Worker %d: %d файли |", w+1, f)
	}
	fmt.Printf("\n📁 Збережено в: %s/\n", d.downloadDir)
}

func main() {
	fmt.Println("📥 ПАРАЛЕЛЬНИЙ DOWNLOADER \n =========================")

	tasks := []DownloadTask{
		{"https://httpbin.org/json", "test_json.json", 1024},
		{"https://httpbin.org/xml", "test_xml.xml", 2048},
		{"https://jsonplaceholder.typicode.com/posts/1", "post_1.json", 1024},
		{"https://jsonplaceholder.typicode.com/users", "users.json", 8192},
		{"https://api.github.com/repos/golang/go", "golang_repo.json", 4096},
		{"https://httpbin.org/delay/2", "delayed_response.json", 1024},
	}
	d := NewDownloader(3)
	d.DownloadFiles(tasks)

}
