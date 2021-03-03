# Project1

**前置学习：**
1. gRPC的使用
2. badger的学习

要点：
1. 需要在standalone_storage中实现StorageReader接口
2. 在`kv/util/engine_util/cf_iterator.go`文件中
   - CFItem实现了DBItem接口
   - BadgerIterator实现了DBIterator接口
3. `kv/util/engine_util/util.go`文件实现了大部分的功能

**待解决：**
1. RPC中err要分别在server端和client端返回吗
2. 如果CF为空，那么是停止运行还是，遍历所有的CF