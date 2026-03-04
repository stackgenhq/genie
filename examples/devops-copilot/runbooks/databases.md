# Databases Runbook

Reference guide for PostgreSQL, MySQL, and ClickHouse query analysis, schema inspection, and performance tuning.

---

## PostgreSQL

### Schema Discovery

```bash
# List databases
psql -h $PG_HOST -U $PG_USER -c "\l"

# List tables in current database
psql -h $PG_HOST -U $PG_USER -d $PG_DB -c "\dt+"

# Describe a table
psql -h $PG_HOST -U $PG_USER -d $PG_DB -c "\d+ <table_name>"

# List indexes
psql -h $PG_HOST -U $PG_USER -d $PG_DB -c "SELECT indexname, tablename, indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY tablename;"
```

### Performance Analysis

```sql
-- Active queries
SELECT pid, now() - pg_stat_activity.query_start AS duration, query, state
FROM pg_stat_activity
WHERE state != 'idle' AND query NOT LIKE '%pg_stat_activity%'
ORDER BY duration DESC;

-- Slow queries (from pg_stat_statements)
SELECT query, calls, mean_exec_time::numeric(10,2) as avg_ms,
  total_exec_time::numeric(10,2) as total_ms
FROM pg_stat_statements
ORDER BY mean_exec_time DESC LIMIT 20;

-- Table bloat estimation
SELECT schemaname, tablename,
  pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as total_size,
  n_dead_tup, n_live_tup,
  round(n_dead_tup::numeric / greatest(n_live_tup, 1) * 100, 2) as dead_pct
FROM pg_stat_user_tables
ORDER BY n_dead_tup DESC LIMIT 20;

-- Index usage
SELECT schemaname, tablename, indexrelname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE idx_scan = 0 ORDER BY pg_relation_size(indexrelid) DESC;

-- Connection stats
SELECT count(*), state FROM pg_stat_activity GROUP BY state;

-- Cache hit ratio (should be > 99%)
SELECT sum(heap_blks_hit) / (sum(heap_blks_hit) + sum(heap_blks_read)) as cache_hit_ratio
FROM pg_statio_user_tables;
```

### Maintenance

```sql
-- Check for tables needing VACUUM
SELECT schemaname, relname, last_vacuum, last_autovacuum, n_dead_tup
FROM pg_stat_user_tables
WHERE n_dead_tup > 10000
ORDER BY n_dead_tup DESC;

-- Replication lag (if replicated)
SELECT application_name, state,
  pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn) as send_lag,
  pg_wal_lsn_diff(sent_lsn, flush_lsn) as flush_lag
FROM pg_stat_replication;
```

---

## MySQL

### Schema Discovery

```bash
# List databases
mysql -h $MYSQL_HOST -u $MYSQL_USER -p -e "SHOW DATABASES;"

# List tables
mysql -h $MYSQL_HOST -u $MYSQL_USER -p $MYSQL_DB -e "SHOW TABLE STATUS;"

# Describe table
mysql -h $MYSQL_HOST -u $MYSQL_USER -p $MYSQL_DB -e "DESCRIBE <table_name>;"
mysql -h $MYSQL_HOST -u $MYSQL_USER -p $MYSQL_DB -e "SHOW CREATE TABLE <table_name>;"
```

### Performance Analysis

```sql
-- Active queries
SHOW FULL PROCESSLIST;

-- InnoDB status
SHOW ENGINE INNODB STATUS\G

-- Slow query analysis (requires slow query log)
SELECT * FROM mysql.slow_log ORDER BY query_time DESC LIMIT 20;

-- Index usage
SELECT table_schema, table_name, index_name, seq_in_index, column_name
FROM information_schema.STATISTICS
WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema')
ORDER BY table_schema, table_name;

-- Table sizes
SELECT table_schema, table_name,
  ROUND((data_length + index_length) / 1024 / 1024, 2) AS size_mb,
  table_rows
FROM information_schema.TABLES
WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema')
ORDER BY (data_length + index_length) DESC LIMIT 20;

-- Connection stats
SHOW STATUS LIKE 'Threads%';
SHOW STATUS LIKE 'Max_used_connections';
```

---

## ClickHouse

### Schema Discovery

```bash
# List databases
clickhouse-client --host $CH_HOST --query "SHOW DATABASES"

# List tables
clickhouse-client --host $CH_HOST --query "SHOW TABLES FROM <database>"

# Describe table
clickhouse-client --host $CH_HOST --query "DESCRIBE TABLE <database>.<table>"
```

### Query Patterns

```sql
-- System query log (recent slow queries)
SELECT query, query_duration_ms, read_rows, read_bytes,
  formatReadableSize(read_bytes) as read_size
FROM system.query_log
WHERE type = 'QueryFinish' AND query_duration_ms > 1000
ORDER BY query_start_time DESC LIMIT 20;

-- Table sizes
SELECT database, table,
  formatReadableSize(sum(bytes_on_disk)) as size,
  sum(rows) as total_rows,
  count() as parts
FROM system.parts
WHERE active
GROUP BY database, table
ORDER BY sum(bytes_on_disk) DESC;
```

---

## Best Practices

- **EXPLAIN before executing** — always `EXPLAIN ANALYZE` new queries
- **Read-only access** — use read replicas when available for analysis
- **Limit result sets** — always use `LIMIT` to avoid overwhelming output
- **Index recommendations** — suggest indexes based on query patterns, don't create without approval
