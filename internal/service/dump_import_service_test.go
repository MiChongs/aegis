package service

import (
	"bufio"
	"strings"
	"testing"
)

// 覆盖：条件注释、行注释、字符串内分号/括号、反斜杠与 ” 转义、NULL、
// 扩展 INSERT、显式列清单 INSERT、零值时间、COMMENT 含中文括号
const sampleDump = "-- MySQL dump 10.13  Distrib 8.0.32\n" +
	"/*!40101 SET @saved_cs_client = @@character_set_client */;\n" +
	"/*!50503 SET NAMES utf8mb4 */;\n" +
	"DROP TABLE IF EXISTS `user`;\n" +
	"CREATE TABLE `user` (\n" +
	"  `id` int(11) NOT NULL AUTO_INCREMENT,\n" +
	"  `appid` int(11) DEFAULT '10000',\n" +
	"  `account` varchar(100) DEFAULT NULL,\n" +
	"  `password` varchar(255) DEFAULT NULL,\n" +
	"  `name` varchar(100) DEFAULT NULL COMMENT '昵称(可空); 历史字段',\n" +
	"  `email` varchar(120) DEFAULT NULL,\n" +
	"  `enabled` tinyint(1) DEFAULT '1',\n" +
	"  `vip_time` bigint(20) DEFAULT '0' COMMENT 'VIP到期(秒)',\n" +
	"  `integral` int(11) DEFAULT '0',\n" +
	"  `open_qq` varchar(64) DEFAULT NULL,\n" +
	"  `register_time` datetime DEFAULT NULL,\n" +
	"  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  UNIQUE KEY `uniq_account` (`appid`,`account`),\n" +
	"  KEY `idx_email` (`email`)\n" +
	") ENGINE=InnoDB AUTO_INCREMENT=4 DEFAULT CHARSET=utf8mb4;\n" +
	"\n" +
	"LOCK TABLES `user` WRITE;\n" +
	"INSERT INTO `user` VALUES " +
	"(1,10000,'alice','$2y$old','爱丽丝; \\'引号\\'','a@x.com',1,999999999,150,'qq_open_1','2023-05-01 10:00:00','2023-05-01 10:00:00')," +
	"(2,10000,'bob','x','Bob\\\\reverse',NULL,0,0,0,NULL,NULL,'2024-01-02 03:04:05');\n" +
	"INSERT INTO `user` (`id`,`appid`,`account`,`password`,`name`,`email`,`enabled`,`vip_time`,`integral`,`open_qq`,`register_time`,`created_at`) " +
	"VALUES (3,20000,'carol''s','h','it''s ok','c@x.com',1,0,5,'','0000-00-00 00:00:00','2024-02-02 00:00:00');\n" +
	"UNLOCK TABLES;\n"

func collectRows(t *testing.T, dump string, table string) []dumpRow {
	t.Helper()
	var rows []dumpRow
	count, err := streamDumpTableRows(bufio.NewReader(strings.NewReader(dump)), table, func(row dumpRow) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if count != len(rows) {
		t.Fatalf("计数不一致: %d != %d", count, len(rows))
	}
	return rows
}

func TestStreamDumpTableRows(t *testing.T) {
	rows := collectRows(t, sampleDump, "user")
	if len(rows) != 3 {
		t.Fatalf("期望 3 行，得到 %d", len(rows))
	}

	// 行 1：转义引号、分号、VIP 永久
	if got := dumpColStr(rows[0], "name"); got != "爱丽丝; '引号'" {
		t.Errorf("行1 name 转义错误: %q", got)
	}
	if got := dumpColInt64(rows[0], "vip_time"); got != 999999999 {
		t.Errorf("行1 vip_time: %d", got)
	}
	if vip := normalizeLegacyVIPTime(dumpColInt64(rows[0], "vip_time")); vip == nil || vip.Year() != 2099 {
		t.Errorf("行1 永久 VIP 归一化失败: %v", vip)
	}
	if ts := dumpColTime(rows[0], "register_time"); ts == nil || ts.Format("2006-01-02") != "2023-05-01" {
		t.Errorf("行1 register_time: %v", ts)
	}

	// 行 2：反斜杠转义、NULL、enabled=0
	if got := dumpColStr(rows[1], "name"); got != "Bob\\reverse" {
		t.Errorf("行2 name 反斜杠: %q", got)
	}
	if rows[1]["email"] != nil {
		t.Errorf("行2 email 应为 NULL")
	}
	if dumpColBool(rows[1], "enabled", true) {
		t.Errorf("行2 enabled 应为 false")
	}

	// 行 3：'' 转义、列清单 INSERT、零值时间
	if got := dumpColStr(rows[2], "account"); got != "carol's" {
		t.Errorf("行3 account '' 转义: %q", got)
	}
	if got := dumpColStr(rows[2], "name"); got != "it's ok" {
		t.Errorf("行3 name '' 转义: %q", got)
	}
	if got := dumpColInt64(rows[2], "appid"); got != 20000 {
		t.Errorf("行3 appid: %d", got)
	}
	if ts := dumpColTime(rows[2], "register_time"); ts != nil {
		t.Errorf("行3 零值时间应为 nil: %v", ts)
	}
}

func TestParseCreateTableColumns(t *testing.T) {
	_, cols := parseCreateTable("CREATE TABLE `user` (\n  `id` int(11) NOT NULL,\n  `name` varchar(10) COMMENT '昵称(中文)括号',\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB")
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "name" {
		t.Fatalf("列解析错误: %v", cols)
	}
}

func TestStatementSplitIgnoresSemicolonInString(t *testing.T) {
	dump := "CREATE TABLE `t` (\n  `a` varchar(10)\n);\n" +
		"INSERT INTO `t` VALUES ('x;y');\n"
	rows := collectRows(t, dump, "t")
	if len(rows) != 1 || dumpColStr(rows[0], "a") != "x;y" {
		t.Fatalf("字符串内分号处理错误: %v", rows)
	}
}

func TestSourceTableFilter(t *testing.T) {
	rows := collectRows(t, sampleDump, "not_exists")
	if len(rows) != 0 {
		t.Fatalf("不存在的表不应有行: %d", len(rows))
	}
}
