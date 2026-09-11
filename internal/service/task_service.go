package service

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/homej-top/dbridge/internal/repository"
	"github.com/homej-top/dbridge/internal/service/drivers"
	"github.com/homej-top/dbridge/pkg/sqlsplit"
	"github.com/homej-top/dbridge/pkg/storage"
	"gorm.io/gorm"
)

// TaskService manages import/export task lifecycle
type TaskService struct {
	db       *gorm.DB
	dsSvc    *DataSourceService
	querySvc *QueryService
	queue    *TaskQueue
	storage  storage.FileStorage
}

// NewTaskService creates a new TaskService
func NewTaskService(db *gorm.DB) *TaskService {
	return &TaskService{
		db:       db,
		dsSvc:    NewDataSourceService(db),
		querySvc: NewQueryService(db),
		queue:    NewTaskQueue(),
		storage:  storage.Get(),
	}
}

// SetStorage sets the default storage backend
func (s *TaskService) SetStorage(st storage.FileStorage) {
	s.storage = st
}

// resolveStorage resolves storage by profile name, falling back to binding config.
// Returns the FileStorage and the base path from binding (empty if profile was explicitly specified).
func resolveStorage(profile string) (storage.FileStorage, string) {
	if profile != "" {
		if st := storage.GetByName(profile); st != nil {
			return st, ""
		}
	}
	b := storage.ResolveModule(storage.ModuleImportExport)
	if b != nil && b.Storage != nil {
		return b.Storage, b.BasePath
	}
	return storage.Get(), ""
}

// ─── Task CRUD ──────────────────────────────────────────────────────────────

// CreateExportTask creates a new export task
func (s *TaskService) CreateExportTask(name, dsID, dbName, schemaName string, cfg *repository.ExportConfig, userID, tenantID string) (*repository.Task, error) {
	task := &repository.Task{
		Name:         name,
		TaskType:     "export",
		DataSourceID: dsID,
		DatabaseName: dbName,
		SchemaName:   schemaName,
		TenantID:     tenantID,
		CreatedBy:    userID,
	}
	if cfg.ExportBatchSize <= 0 {
		cfg.ExportBatchSize = 1000
	}
	if cfg.ExportFormat == "" {
		cfg.ExportFormat = "sql"
	}
	if cfg.ExportContent == "" {
		cfg.ExportContent = "all"
	}
	if cfg.ExportScope == "" {
		cfg.ExportScope = "tables"
	}
	if err := task.SetExportConfig(cfg); err != nil {
		return nil, err
	}
	if err := s.db.Create(task).Error; err != nil {
		return nil, fmt.Errorf("failed to create export task: %w", err)
	}
	return task, nil
}

// CreateImportTask creates a new import task
func (s *TaskService) CreateImportTask(name, dsID, dbName, schemaName string, cfg *repository.ImportConfig, userID, tenantID string) (*repository.Task, error) {
	task := &repository.Task{
		Name:         name,
		TaskType:     "import",
		DataSourceID: dsID,
		DatabaseName: dbName,
		SchemaName:   schemaName,
		TenantID:     tenantID,
		CreatedBy:    userID,
	}
	if cfg.ImportStrategy == "" {
		cfg.ImportStrategy = "fail"
	}
	if cfg.ImportSource == "" {
		cfg.ImportSource = "upload"
	}
	if cfg.ImportContent == "" {
		cfg.ImportContent = "all"
	}
	if err := task.SetImportConfig(cfg); err != nil {
		return nil, err
	}
	if err := s.db.Create(task).Error; err != nil {
		return nil, fmt.Errorf("failed to create import task: %w", err)
	}
	return task, nil
}

// GetTask retrieves a task by ID
func (s *TaskService) GetTask(id string) (*repository.Task, error) {
	var task repository.Task
	if err := s.db.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	// Populate transient status from latest execution
	exec, err := s.GetLatestExecution(id)
	if err == nil {
		task.Status = exec.Status
	}
	return &task, nil
}

// ListTasks lists tasks with optional filters
func (s *TaskService) ListTasks(tenantID string, taskType string) ([]repository.Task, error) {
	var tasks []repository.Task
	q := s.db.Order("created_at DESC")
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if taskType != "" {
		q = q.Where("task_type = ?", taskType)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	// Populate status from latest execution for each task
	for i := range tasks {
		exec, err := s.GetLatestExecution(tasks[i].ID)
		if err == nil {
			tasks[i].Status = exec.Status
		}
	}
	return tasks, nil
}

// DeleteTask deletes a task and its executions
func (s *TaskService) DeleteTask(id string) error {
	s.db.Where("task_id = ?", id).Delete(&repository.TaskExecution{})
	return s.db.Delete(&repository.Task{}, "id = ?", id).Error
}

// ─── Execution ──────────────────────────────────────────────────────────────

// StartExecution creates a new execution record and queues it for running
func (s *TaskService) StartExecution(taskID string) (*repository.TaskExecution, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	// Get last run number
	var lastExec repository.TaskExecution
	runNumber := 1
	if err := s.db.Where("task_id = ?", taskID).Order("run_number DESC").First(&lastExec).Error; err == nil {
		runNumber = lastExec.RunNumber + 1
	}

	exec := &repository.TaskExecution{
		TaskID:    taskID,
		RunNumber: runNumber,
		Status:    "pending",
		Progress:  0,
	}
	if err := s.db.Create(exec).Error; err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	// Submit to queue
	if err := s.queue.Submit(task, exec); err != nil {
		exec.Status = "failed"
		exec.ErrorMsg = err.Error()
		s.db.Save(exec)
		return exec, err
	}

	return exec, nil
}

// CancelExecution cancels a running execution
func (s *TaskService) CancelExecution(execID string) error {
	s.queue.Cancel(execID)
	return s.db.Model(&repository.TaskExecution{}).Where("id = ?", execID).
		Update("status", "cancelled").Error
}

// GetLatestExecution returns the most recent execution for a task
func (s *TaskService) GetLatestExecution(taskID string) (*repository.TaskExecution, error) {
	var exec repository.TaskExecution
	err := s.db.Where("task_id = ?", taskID).Order("run_number DESC").First(&exec).Error
	return &exec, err
}

// GetExecution retrieves an execution by ID
func (s *TaskService) GetExecution(id string) (*repository.TaskExecution, error) {
	var exec repository.TaskExecution
	err := s.db.Where("id = ?", id).First(&exec).Error
	return &exec, err
}

// ListExecutions lists all executions for a task
func (s *TaskService) ListExecutions(taskID string) ([]repository.TaskExecution, error) {
	var execs []repository.TaskExecution
	err := s.db.Where("task_id = ?", taskID).Order("run_number DESC").Find(&execs).Error
	return execs, err
}

// ─── Task Queue ─────────────────────────────────────────────────────────────

// TaskQueue controls concurrent execution per data source
type TaskQueue struct {
	mu        sync.Mutex
	dsRunning map[string]int // data source ID → running count
	maxPerDS  int            // max concurrent per data source
	waiting   []*queueItem
	cancelled map[string]bool // execution IDs marked for cancel
	runners   map[string]context.CancelFunc
}

type queueItem struct {
	task *repository.Task
	exec *repository.TaskExecution
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		dsRunning: make(map[string]int),
		maxPerDS:  2,
		cancelled: make(map[string]bool),
		runners:   make(map[string]context.CancelFunc),
	}
}

func (q *TaskQueue) Submit(task *repository.Task, exec *repository.TaskExecution) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.dsRunning[task.DataSourceID] >= q.maxPerDS {
		return fmt.Errorf("数据源任务繁忙，当前已有 %d 个任务在运行，请稍后重试", q.dsRunning[task.DataSourceID])
	}
	q.dsRunning[task.DataSourceID]++
	ctx, cancel := context.WithCancel(context.Background())
	q.runners[exec.ID] = cancel
	go q.run(ctx, task, exec)
	return nil
}

func (q *TaskQueue) Cancel(execID string) {
	q.mu.Lock()
	q.cancelled[execID] = true
	if cancel, ok := q.runners[execID]; ok {
		cancel()
	}
	q.mu.Unlock()
}

func (q *TaskQueue) run(ctx context.Context, task *repository.Task, exec *repository.TaskExecution) {
	defer func() {
		// 捕获 panic：单个任务执行异常不能导致整个服务崩溃（如存储未配置等运行时错误）
		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("任务执行发生内部错误: %v", r)
			exec.Status = "failed"
			exec.ErrorMsg = errMsg
			exec.LogText += "\n[ERROR] " + errMsg
			if db := repository.GetDB(); db != nil {
				db.Save(exec)
			}
		}
		q.mu.Lock()
		q.dsRunning[task.DataSourceID]--
		if q.dsRunning[task.DataSourceID] < 0 {
			q.dsRunning[task.DataSourceID] = 0
		}
		delete(q.runners, exec.ID)
		q.mu.Unlock()
	}()

	// Mark as running
	db := repository.GetDB()
	now := time.Now()
	exec.Status = "running"
	exec.StartedAt = &now
	db.Save(exec)

	var err error
	switch task.TaskType {
	case "export":
		err = q.executeExport(ctx, task, exec, db)
	case "import":
		err = q.executeImport(ctx, task, exec, db)
	default:
		err = fmt.Errorf("unknown task type: %s", task.TaskType)
	}

	finishedAt := time.Now()
	exec.FinishedAt = &finishedAt
	if err != nil {
		exec.Status = "failed"
		exec.ErrorMsg = err.Error()
		exec.LogText += "\n[ERROR] " + err.Error()
	} else {
		exec.Status = "completed"
		exec.Progress = 100
	}
	db.Save(exec)
}

// ─── Export Execution ──────────────────────────────────────────────────────

// connectTaskDriver 连接任务对应的数据源，支持 PG/MSSQL 指定 database
func (q *TaskQueue) connectTaskDriver(db *gorm.DB, task *repository.Task) (drivers.DatabaseDriver, *repository.DataSource, error) {
	var dsRecord repository.DataSource
	if err := db.Where("id = ?", task.DataSourceID).First(&dsRecord).Error; err != nil {
		return nil, nil, fmt.Errorf("数据源不存在: %w", err)
	}

	dsSvc := NewDataSourceService(db)
	// PG/MSSQL 双层结构：需要连接指定 database
	if task.DatabaseName != "" && task.DatabaseName != dsRecord.Database {
		driver, err := dsSvc.ConnectForDB(task.DataSourceID, task.DatabaseName)
		if err != nil {
			return nil, nil, err
		}
		return driver, &dsRecord, nil
	}
	driver, _, ds, err := dsSvc.Connect(task.DataSourceID)
	return driver, ds, err
}

func (q *TaskQueue) executeExport(ctx context.Context, task *repository.Task, exec *repository.TaskExecution, db *gorm.DB) error {
	expCfg, err := task.GetExportConfig()
	if err != nil {
		return fmt.Errorf("解析导出配置失败: %w", err)
	}

	// Connect to data source
	driver, ds, err := q.connectTaskDriver(db, task)
	if err != nil {
		return fmt.Errorf("连接数据源失败: %w", err)
	}
	defer driver.Close()

	schema := task.SchemaName
	if schema == "" {
		schema = ds.Database
	}

	// Determine tables
	tableItems := make([]drivers.TableListItem, 0)
	if expCfg.ExportScope == "schema" || len(expCfg.ExportTables) == 0 {
		allTables, err := driver.ListTables(schema)
		if err != nil {
			return fmt.Errorf("获取表列表失败: %w", err)
		}
		for _, t := range allTables {
			if t.Type == "table" {
				tableItems = append(tableItems, t)
			}
		}
	} else {
		// Use specified tables
		tableNames := expCfg.ExportTables
		for _, name := range tableNames {
			tableItems = append(tableItems, drivers.TableListItem{Name: name, Type: "table"})
		}
	}

	// Filter system tables
	tableItems = filterSystemTables(ds.Type, tableItems)

	tables := make([]string, len(tableItems))
	for i, t := range tableItems {
		tables[i] = t.Name
	}

	if len(tables) == 0 {
		return fmt.Errorf("没有找到需要导出的表")
	}

	// Determine storage
	fileStorage, basePath := resolveStorage(expCfg.StorageProfile)
	if fileStorage == nil {
		return fmt.Errorf("存储实例不可用，请先配置存储（系统设置-文件存储）")
	}

	// Build file path
	storagePath := expCfg.StoragePath
	if storagePath == "" {
		if basePath != "" {
			storagePath = basePath + "/"
		} else {
			storagePath = "exports/"
		}
	}
	fileName := fmt.Sprintf("%s_%s_%s.sql",
		strings.ReplaceAll(ds.Name, " ", "_"),
		schema,
		time.Now().Format("20060102_150405"))
	filePath := filepath.Join(storagePath, task.ID, fileName)

	// Use a pipe for streaming: producer writes SQL, consumer uploads to storage
	pr, pw := io.Pipe()
	uploadDone := make(chan error, 1)

	go func() {
		info, err := fileStorage.Save(ctx, filePath, pr, "application/sql")
		if err != nil {
			uploadDone <- err
			return
		}
		exec.ResultFilePath = info.Path
		exec.ResultFileName = info.Name
		exec.ResultFileSize = info.Size
		uploadDone <- nil
	}()

	// Write header
	header := fmt.Sprintf("-- DBridge Export\n-- Source: %s (%s)\n-- Schema: %s\n-- Generated: %s\n\n",
		ds.Name, ds.Type, schema, time.Now().Format("2006-01-02 15:04:05"))
	if _, err := pw.Write([]byte(header)); err != nil {
		pw.CloseWithError(err)
		return err
	}

	// Write foreign key disable header
	if ds.Type == "mysql" || ds.Type == "mariadb" {
		pw.Write([]byte("SET FOREIGN_KEY_CHECKS = 0;\n\n"))
	}

	totalTables := len(tables)
	exec.LogText += fmt.Sprintf("[INFO] 开始导出，共 %d 张表，内容: %s\n", totalTables, expCfg.ExportContent)
	wroteAny := false

	for i, table := range tables {
		select {
		case <-ctx.Done():
			pw.CloseWithError(ctx.Err())
			return ctx.Err()
		default:
		}

		// Export structure
		if expCfg.ExportContent == "all" || expCfg.ExportContent == "structure" {
			ddl, err := driver.GetDDL(schema, table)
			if err != nil {
				exec.LogText += fmt.Sprintf("\n[WARN] 获取表 %s DDL 失败: %v", table, err)
			} else if ddl != "" {
				ddl = strings.TrimSpace(ddl)
				if !strings.HasSuffix(ddl, ";") {
					ddl += ";"
				}
				pw.Write([]byte(fmt.Sprintf("\n-- ========================================\n-- Table: %s\n-- ========================================\n\n", table)))
				pw.Write([]byte(ddl))
				pw.Write([]byte("\n"))
				wroteAny = true
				exec.LogText += fmt.Sprintf("[INFO] 表 %s 结构导出完成\n", table)
			}
		}

		// Export data
		if expCfg.ExportContent == "all" || expCfg.ExportContent == "data" {
			rows, err := q.exportTableData(ctx, driver, table, schema, expCfg.ExportBatchSize, pw)
			if err != nil {
				exec.LogText += fmt.Sprintf("\n[WARN] 导出表 %s 数据失败: %v", table, err)
			} else {
				if rows > 0 {
					wroteAny = true
				}
				exec.LogText += fmt.Sprintf("[INFO] 表 %s 数据导出完成，共 %d 行\n", table, rows)
			}
		}

		// Update progress
		exec.Progress = int(float64(i+1) / float64(totalTables) * 100)
		db.Save(exec)
	}

	if !wroteAny {
		err := fmt.Errorf("导出内容为空：没有可导出的表结构或数据（请检查数据源、Schema 或所选表）")
		pw.CloseWithError(err)
		return err
	}

	exec.LogText += fmt.Sprintf("[INFO] 导出完成，共 %d 张表\n", totalTables)

	// Write foreign key enable footer
	if ds.Type == "mysql" || ds.Type == "mariadb" {
		pw.Write([]byte("\nSET FOREIGN_KEY_CHECKS = 1;\n"))
	}

	pw.Close()
	return <-uploadDone
}

func (q *TaskQueue) exportTableData(ctx context.Context, driver drivers.DatabaseDriver, table, schema string, batchSize int, w io.Writer) (int, error) {
	page := 1
	totalRows := 0
	for {
		select {
		case <-ctx.Done():
			return totalRows, ctx.Err()
		default:
		}

		// 使用 driver 的 GetTableData 分页获取（内部已处理各方言分页 SQL）
		result, err := driver.GetTableData(schema, table, page, batchSize)
		if err != nil {
			return totalRows, fmt.Errorf("查询表 %s 数据失败 (page=%d): %w", table, page, err)
		}

		if result.Rows == nil || len(result.Rows) == 0 {
			break
		}

		// 引用列名
		quotedCols := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			quotedCols[i] = driver.SQLQuoteIdent(col)
		}
		colList := strings.Join(quotedCols, ", ")

		// 引用表名（不带 schema 前缀，与 CREATE TABLE 保持一致）
		quotedTable := driver.SQLQuoteIdent(table)

		// 格式化每行值，生成多值 INSERT
		rowValues := make([]string, 0, len(result.Rows))
		for _, row := range result.Rows {
			vals := make([]string, len(result.Columns))
			for i := range result.Columns {
				var val interface{}
				if i < len(row) {
					val = row[i]
				}
				vals[i] = driver.FormatSQLValue(val)
			}
			rowValues = append(rowValues, "("+strings.Join(vals, ", ")+")")
		}

		insert := driver.BuildInsertSQL(quotedTable, colList, rowValues)
		if _, err := w.Write([]byte(insert + "\n")); err != nil {
			return totalRows, err
		}

		totalRows += len(result.Rows)
		page++
		if len(result.Rows) < batchSize {
			break
		}
	}
	return totalRows, nil
}

// ─── Import Execution ──────────────────────────────────────────────────────

func (q *TaskQueue) executeImport(ctx context.Context, task *repository.Task, exec *repository.TaskExecution, db *gorm.DB) error {
	impCfg, err := task.GetImportConfig()
	if err != nil {
		return fmt.Errorf("解析导入配置失败: %w", err)
	}

	// Connect to target data source
	driver, ds, err := q.connectTaskDriver(db, task)
	if err != nil {
		return fmt.Errorf("连接目标数据源失败: %w", err)
	}
	defer driver.Close()

	schema := task.SchemaName
	if schema == "" && impCfg.TargetSchema != "" {
		schema = impCfg.TargetSchema
	}

	exec.LogText += fmt.Sprintf("[INFO] 开始导入 → 数据源: %s (%s), Schema: %s, 来源: %s, 内容: %s, 策略: %s",
		ds.Name, ds.Type, schema, impCfg.ImportSource, impCfg.ImportContent, impCfg.ImportStrategy)
	db.Save(exec)

	// Get SQL reader based on import source
	var reader io.ReadCloser
	var totalBytes int64 // 文件源用于进度估算
	switch impCfg.ImportSource {
	case "upload", "storage":
		fileStorage, _ := resolveStorage(impCfg.StorageProfile)
		if fileStorage == nil {
			return fmt.Errorf("存储实例不存在: %s", impCfg.StorageProfile)
		}
		// 安全检查：先流式扫描一遍文件检测危险语句（不整体加载到内存）
		if !impCfg.SkipSafetyCheck {
			scanR, scanErr := fileStorage.ReadStream(ctx, impCfg.ImportFilePath)
			if scanErr != nil {
				return fmt.Errorf("读取导入文件失败 (%s): %w", impCfg.ImportFilePath, scanErr)
			}
			dangerErr := scanImportForDangerous(scanR, ds.Type)
			scanR.Close()
			if dangerErr != nil {
				return dangerErr
			}
		}
		if info, statErr := fileStorage.Stat(ctx, impCfg.ImportFilePath); statErr == nil && info != nil {
			totalBytes = info.Size
		}
		reader, err = fileStorage.ReadStream(ctx, impCfg.ImportFilePath)
		if err != nil {
			return fmt.Errorf("读取导入文件失败 (%s): %w", impCfg.ImportFilePath, err)
		}
		defer reader.Close()
	case "db2db":
		// For db2db, we generate SQL stream from source on-the-fly
		reader, err = q.generateDB2DBStream(ctx, task, impCfg, db)
		if err != nil {
			return fmt.Errorf("db2db 流初始化失败: %w", err)
		}
		defer reader.Close()
	default:
		return fmt.Errorf("不支持的导入来源: %s", impCfg.ImportSource)
	}

	// 流式解析并逐条执行：解析出一条立即执行，不将整个文件读入内存
	exec.LogText += "\n[INFO] 开始流式解析并执行 SQL"
	db.Save(exec)

	cr := &countingReader{r: stripBOMReader(reader)}
	var stmtCount, ddlCount, dmlCount int

	// 源库名：用于去除导出 SQL 中的库名前缀，确保导入目标 schema 正确
	sourceSchema := impCfg.SourceDatabase
	if sourceSchema == "" {
		sourceSchema = impCfg.SourceSchema
	}
	schemaDetected := sourceSchema != ""

	streamErr := sqlsplit.SplitStream(cr, ds.Type, func(stmt string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 自动检测源库名（从第一条含库名前缀的语句中提取）
		if !schemaDetected {
			if detected := detectSourceSchema(stmt, ds.Type); detected != "" {
				sourceSchema = detected
				exec.LogText += fmt.Sprintf("\n[INFO] 检测到源库名前缀: `%s`，导入时将自动去除", sourceSchema)
			}
			schemaDetected = true
		}

		// 去除源库名前缀，确保语句在目标 schema 中执行
		stmt = stripSchemaPrefix(stmt, sourceSchema, ds.Type)

		stmtCount++

		// 安全检查（仅检查当前这一条）
		if !impCfg.SkipSafetyCheck {
			if err := dangerousStmtError(stmt); err != nil {
				return err
			}
		}

		upper := strings.ToUpper(strings.TrimSpace(stmt))
		if strings.HasPrefix(upper, "CREATE") || strings.HasPrefix(upper, "ALTER") || strings.HasPrefix(upper, "DROP") {
			ddlCount++
			if _, err := driver.ExecuteQuery(stmt, schema); err != nil {
				appendImportLog(exec, fmt.Sprintf("\n[WARN] DDL 执行失败 (继续): %s\n语句: %s", err.Error(), truncateStr(stmt, 100)))
			} else {
				appendImportLog(exec, fmt.Sprintf("\n[INFO] DDL 执行成功: %s", truncateStr(stmt, 60)))
			}
		} else {
			dmlCount++
			execStmt := applyImportStrategy(stmt, impCfg.ImportStrategy, ds.Type)
			if _, err := driver.ExecuteQuery(execStmt, schema); err != nil {
				switch impCfg.ImportStrategy {
				case "fail":
					return fmt.Errorf("DML 执行失败 (fail 策略，已中止): %s\n语句: %s", err.Error(), truncateStr(stmt, 100))
				case "skip":
					appendImportLog(exec, fmt.Sprintf("\n[SKIP] DML 执行失败 (跳过): %s\n语句: %s", err.Error(), truncateStr(stmt, 100)))
				default: // replace
					appendImportLog(exec, fmt.Sprintf("\n[WARN] DML 执行失败 (replace 策略): %s\n语句: %s", err.Error(), truncateStr(stmt, 100)))
				}
			} else {
				appendImportLog(exec, fmt.Sprintf("\n[INFO] DML 执行成功 (%d): %s", stmtCount, truncateStr(stmt, 60)))
			}
		}

		// 定期持久化进度与日志，避免频繁写库
		if stmtCount%100 == 0 {
			exec.Progress = importProgress(cr.n, totalBytes)
			db.Save(exec)
		}
		return nil
	})

	if streamErr != nil {
		return streamErr
	}

	if stmtCount == 0 {
		if impCfg.ImportFilePath != "" {
			return fmt.Errorf("文件中没有找到有效的 SQL 语句（文件可能为空或仅包含注释）: %s", impCfg.ImportFilePath)
		}
		return fmt.Errorf("没有找到有效的 SQL 语句（源库可能没有可导入的表或数据）")
	}

	appendImportLog(exec, fmt.Sprintf("\n[INFO] 导入完成，共处理 %d 条语句 (DDL: %d, DML: %d)", stmtCount, ddlCount, dmlCount))
	exec.Progress = 100
	db.Save(exec)
	return nil
}

// applyImportStrategy rewrites INSERT statements to handle duplicates
// based on the import strategy and target dialect.
func applyImportStrategy(stmt string, strategy string, dialect string) string {
	if strategy == "fail" || strategy == "" {
		return stmt
	}

	trimmed := strings.TrimSpace(stmt)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "INSERT") {
		return stmt
	}

	switch strategy {
	case "skip":
		switch dialect {
		case "mysql", "mariadb", "oceanbase":
			return rewriteInsertPrefix(trimmed, "INSERT INTO", "INSERT IGNORE INTO")
		case "sqlite":
			return rewriteInsertPrefix(trimmed, "INSERT INTO", "INSERT OR IGNORE INTO")
		}
	case "replace":
		switch dialect {
		case "mysql", "mariadb", "oceanbase":
			return rewriteInsertPrefix(trimmed, "INSERT INTO", "REPLACE INTO")
		case "sqlite":
			return rewriteInsertPrefix(trimmed, "INSERT INTO", "INSERT OR REPLACE INTO")
		}
	}
	return stmt
}

// rewriteInsertPrefix replaces the first occurrence of "INSERT INTO" (case-insensitive)
// with the given replacement, preserving the rest of the statement.
func rewriteInsertPrefix(stmt string, from string, to string) string {
	idx := strings.Index(strings.ToUpper(stmt), from)
	if idx < 0 {
		return stmt
	}
	return stmt[:idx] + to + stmt[idx+len(from):]
}

// detectSourceSchema scans SQL text for a schema-qualified table reference
// and returns the schema name (unquoted). Returns "" if none found.
func detectSourceSchema(sql string, dialect string) string {
	q, close := quoteChars(dialect)
	// Escape regex-special characters in quote chars (e.g. [ ] for MSSQL)
	qRe := regexp.QuoteMeta(q)
	closeRe := regexp.QuoteMeta(close)

	// Match: quoted_schema . quoted_name  after CREATE TABLE [IF NOT EXISTS] or INSERT INTO
	pattern := `(?i)(?:CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|INSERT\s+INTO)\s*` +
		qRe + `([^` + close + `]+)` + closeRe +
		`\s*\.\s*` +
		qRe + `[^` + close + `]+` + closeRe
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(sql)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// stripSchemaPrefix removes a known schema prefix from a SQL statement.
// E.g. with sourceSchema="dbbridge" and dialect="mysql":
//
//	"INSERT INTO `dbbridge`.`t` ..." → "INSERT INTO `t` ..."
//	"CREATE TABLE `dbbridge`.`t` ..." → "CREATE TABLE `t` ..."
func stripSchemaPrefix(stmt string, sourceSchema string, dialect string) string {
	if sourceSchema == "" {
		return stmt
	}
	q, close := quoteChars(dialect)
	quoted := q + sourceSchema + close + "."
	return strings.ReplaceAll(stmt, quoted, "")
}

// quoteChars returns the open and close quote characters for identifiers in the given dialect.
func quoteChars(dialect string) (string, string) {
	switch dialect {
	case "mysql", "mariadb", "oceanbase":
		return "`", "`"
	case "sqlserver", "mssql":
		return "[", "]"
	default:
		return `"`, `"`
	}
}

func (q *TaskQueue) generateDB2DBStream(ctx context.Context, task *repository.Task, impCfg *repository.ImportConfig, db *gorm.DB) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		dsSvc := NewDataSourceService(db)
		// 支持 PG/MSSQL 指定源 database
		var driver drivers.DatabaseDriver
		var srcDS *repository.DataSource
		var err error
		if impCfg.SourceDatabase != "" {
			var srcRecord repository.DataSource
			if e := db.Where("id = ?", impCfg.SourceDSID).First(&srcRecord).Error; e == nil && impCfg.SourceDatabase != srcRecord.Database {
				driver, err = dsSvc.ConnectForDB(impCfg.SourceDSID, impCfg.SourceDatabase)
				srcDS = &srcRecord
			} else {
				var _db *sql.DB
				driver, _db, srcDS, err = dsSvc.Connect(impCfg.SourceDSID)
				if _db != nil {
					// close handled by defer driver.Close()
				}
			}
		} else {
			var _db *sql.DB
			driver, _db, srcDS, err = dsSvc.Connect(impCfg.SourceDSID)
			if _db != nil {
				// close handled by defer driver.Close()
			}
		}
		if err != nil {
			pw.CloseWithError(fmt.Errorf("连接源数据源失败: %w", err))
			return
		}
		defer driver.Close()

		srcSchema := impCfg.SourceSchema
		if srcSchema == "" {
			srcSchema = srcDS.Database
		}

		tables := impCfg.SourceTables
		if len(tables) == 0 {
			allTables, err := driver.ListTables(srcSchema)
			if err == nil {
				for _, t := range allTables {
					if t.Type == "table" {
						tables = append(tables, t.Name)
					}
				}
			}
		}
		if len(tables) == 0 {
			pw.CloseWithError(fmt.Errorf("源库中没有找到可导入的表"))
			return
		}

		for _, table := range tables {
			if impCfg.ImportContent == "all" || impCfg.ImportContent == "structure" {
				ddl, err := driver.GetDDL(srcSchema, table)
				if err == nil && ddl != "" {
					ddl = strings.TrimSpace(ddl)
					if !strings.HasSuffix(ddl, ";") {
						ddl += ";"
					}
					pw.Write([]byte(ddl + "\n"))
				}
			}
			if impCfg.ImportContent == "all" || impCfg.ImportContent == "data" {
				if _, err := q.exportTableData(ctx, driver, table, srcSchema, 1000, pw); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return pr, nil
}

// ─── SQL Parsing (streaming) ──────────────────────────────────────────────

// countingReader 统计已读取字节数，用于估算导入进度
// （流式解析不会把整个文件读入内存，内存占用与文件大小无关）
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// stripBOMReader 去除 UTF-8 BOM（\xEF\xBB\xBF）
func stripBOMReader(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if peek, err := br.Peek(3); err == nil && len(peek) >= 3 && peek[0] == 0xEF && peek[1] == 0xBB && peek[2] == 0xBF {
		br.Discard(3)
	}
	return br
}

// importProgress 根据已读字节数估算进度百分比（无法估算时返回 0）
func importProgress(read, total int64) int {
	if total <= 0 {
		return 0
	}
	pct := int(read * 100 / total)
	if pct < 0 {
		pct = 0
	}
	if pct > 99 {
		pct = 99
	}
	return pct
}

// scanImportForDangerous 流式扫描 SQL，检测危险语句（不整体加载到内存）
func scanImportForDangerous(reader io.Reader, dialect string) error {
	return sqlsplit.SplitStream(stripBOMReader(reader), dialect, func(stmt string) error {
		return dangerousStmtError(stmt)
	})
}

// ─── Safety Check ───────────────────────────────────────────────────────────

var dangerousPatterns = []string{
	"DROP DATABASE", "DROP TABLE", "TRUNCATE TABLE",
	"DROP SCHEMA", "DROP ALL",
}

func dangerousStmtError(stmt string) error {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	for _, pattern := range dangerousPatterns {
		if strings.Contains(upper, pattern) {
			return fmt.Errorf("检测到危险语句 [%s]，已阻止执行。如需继续请跳过安全检查。\n语句: %s",
				pattern, truncateStr(stmt, 100))
		}
	}
	return nil
}

// appendImportLog 追加日志并限制总长度，防止超大导入导致内存/DB 膨胀
const maxImportLogSize = 200 * 1024 // 200KB

func appendImportLog(exec *repository.TaskExecution, line string) {
	exec.LogText += line
	if len(exec.LogText) > maxImportLogSize {
		exec.LogText = "...(日志过长，仅保留最近部分)...\n" + exec.LogText[len(exec.LogText)-maxImportLogSize:]
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// 避免截断在 UTF-8 多字节字符中间，导致日志/提示出现乱码
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func filterSystemTables(dbType string, tables []drivers.TableListItem) []drivers.TableListItem {
	systemSchemas := map[string]map[string]bool{
		"mysql":      {"information_schema": true, "mysql": true, "performance_schema": true, "sys": true},
		"mariadb":    {"information_schema": true, "mysql": true, "performance_schema": true, "sys": true},
		"postgres":   {"pg_catalog": true, "information_schema": true},
		"postgresql": {"pg_catalog": true, "information_schema": true},
		"mssql":      {"sys": true, "INFORMATION_SCHEMA": true},
		"sqlserver":  {"sys": true, "INFORMATION_SCHEMA": true},
		"oracle":     {"SYS": true, "SYSTEM": true, "XDB": true},
	}
	exclude := systemSchemas[strings.ToLower(dbType)]
	if exclude == nil {
		return tables
	}
	var filtered []drivers.TableListItem
	for _, t := range tables {
		if !exclude[strings.ToLower(t.Name)] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ─── Storage File Browse ────────────────────────────────────────────────────

// StorageFileEntry represents a file/directory entry in a storage browser
type StorageFileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"` // file | directory
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// BrowseStorageFiles lists files in a storage profile directory
func (s *TaskService) BrowseStorageFiles(profile, path string) ([]StorageFileEntry, error) {
	fileStorage, _ := resolveStorage(profile)
	if fileStorage == nil {
		return nil, fmt.Errorf("存储实例 '%s' 不存在", profile)
	}

	dir := path
	if dir == "" {
		dir = "/"
	}

	result, err := fileStorage.List(context.Background(), dir, &storage.ListOptions{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, fmt.Errorf("浏览目录失败: %w", err)
	}

	var entries []StorageFileEntry
	for _, f := range result.Files {
		entryType := "file"
		if f.IsDir {
			entryType = "directory"
		}
		entries = append(entries, StorageFileEntry{
			Name:       f.Name,
			Path:       f.Path,
			Type:       entryType,
			Size:       f.Size,
			ModifiedAt: f.ModTime,
		})
	}
	return entries, nil
}

// ─── Cleanup ────────────────────────────────────────────────────────────────

// CleanupLoop runs periodic cleanup of old export/import files
func (s *TaskService) CleanupLoop(ctx context.Context, interval time.Duration, retentionDays int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpiredFiles(retentionDays)
		}
	}
}

func (s *TaskService) cleanupExpiredFiles(retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	var execs []repository.TaskExecution
	s.db.Where("status = 'completed' AND created_at < ? AND result_file_path != ''", cutoff).Find(&execs)

	for _, exec := range execs {
		fileStorage := storage.Get()
		if fileStorage == nil {
			continue
		}
		if exec.ResultFilePath != "" {
			fileStorage.Delete(context.Background(), exec.ResultFilePath)
		}
		if exec.LogFilePath != "" {
			fileStorage.Delete(context.Background(), exec.LogFilePath)
		}
		// Clear paths after deletion
		s.db.Model(&exec).Updates(map[string]interface{}{
			"result_file_path": "",
			"log_file_path":    "",
		})
	}
}
