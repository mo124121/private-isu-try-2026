# ISUCON preparation repository

ISUCON14 参加時の「競技開始前」リポジトリを基準にした、当日作業用の雛形です。
問題固有のパス、サービス名、nginx/MySQL 設定は、競技環境を確認してから追加します。

## 開始直後に設定するもの

1. `ansible/inventory.yaml` の接続先、ユーザー、内部 IP を設定する。
2. 採用言語のソースを `webapp/` 以下へ回収する。
3. `webapp_source_dir`、`webapp_deploy_dir`、`webapp_binary_name`、
   `webapp_service_name` を実機の構成に合わせる。
4. 実機の nginx/MySQL 設定を回収し、確認後に Ansible role 化する。

`webapp_service_name` は意図しないサービスを再起動しないよう、初期値では
デプロイが失敗する値にしています。

## よく使うコマンド

```sh
cd ansible

# 疎通確認
ansible all -m ping

# 内容を確認（対象ホストは変更しない）
ansible-playbook web.yaml --syntax-check
ansible-playbook web.yaml --check --diff

# Go バイナリをローカルビルドし、1台ずつ配布・再起動
ansible-playbook web.yaml --diff

# 個別グループへ限定する場合
ansible-playbook web.yaml --limit webapp01 --diff
```

## 構成

- `ansible/web.yaml`: Linux 向けビルド、バイナリ・テンプレート配布、systemd 再起動
- `ansible/nginx.yaml`: 当日回収した設定を追加するための入口
- `ansible/db.yaml`: 当日回収した設定を追加するための入口
- `ansible/all.yaml`: 確認済み playbook の一括実行用
- `go/isuutil`: Go 実装へ必要なものだけ取り込むための持ち込み用ユーティリティ
- `webapp/`: 競技開始後に回収するアプリケーション（開始前は未配置）

監視基盤は別途構築する前提のため、このリポジトリでは管理しません。
