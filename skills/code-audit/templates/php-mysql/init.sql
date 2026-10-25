-- 本地靶场种子数据：表结构按被审计应用的真实 schema 替换/补充。
-- 数据库名由 compose 的 MYSQL_DATABASE（LAB_DB_NAME）创建，这里不要写 CREATE DATABASE/USE。
CREATE TABLE IF NOT EXISTS users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  password VARCHAR(128) NOT NULL,
  role VARCHAR(32) NOT NULL DEFAULT 'user'
);

INSERT INTO users (username, password, role) VALUES
  ('admin', 'admin123', 'admin'),
  ('alice', 'alice123', 'user'),
  ('bob', 'bob123', 'user');
