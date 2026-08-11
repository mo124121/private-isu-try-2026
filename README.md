# isucon-14

ISUCON14のコンテストコード

## 本番作業用リンク集

- [pprotein](http://isucon-o11y:9000)  
- [jaeger](http://isucon-o11y:16686)  
- [prometheus + クエリ](http://isucon-o11y:9090/query?g0.expr=100+-+(avg+by+(instance)+(irate(node_cpu_seconds_total%7Bmode%3D%22idle%22%7D%5B5m%5D))+*+100)&g0.show_tree=0&g0.tab=graph&g0.range_input=15m&g0.res_type=auto&g0.res_density=medium&g0.display_mode=lines&g0.show_exemplars=0&g1.expr=irate(namedprocess_namegroup_cpu_seconds_total%5B5m%5D)&g1.show_tree=0&g1.tab=graph&g1.range_input=15m&g1.res_type=auto&g1.res_density=medium&g1.display_mode=lines&g1.show_exemplars=0)  

## スニペット集

```sh
# ansible playbookの実行
cd ansible
ansible-playbook -i inventory.yaml web.yaml

# sshログイン
ssh ubuntu@server1
ssh isucon@server1 #ユーザ次第

# ログの閲覧
journalctl -xeu isuconxxx-go #サービス名は注意 eオプションで一番最後に飛ぶ
##ログを検索したいときは/を押して続ければいい、viスタイル

# サービスの操作
sudo systemctl cat isuconxxx-go #サービス定義ファイルの確認
sudo systemctl restart isuconxx-go #再起動
```

環境によって微妙に変わるので適宜読み替えること

## メモ

issue切るときに、pproteinなら測定結果のリンクをそのままはればみんなで確認できる。  
jaegerとprometheusは流れて行ってしまうのでスクショとる方がいいかも。
