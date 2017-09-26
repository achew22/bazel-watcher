load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "command.go",
        "default_command.go",
    ],
    importpath = "github.com/bazelbuild/bazel-watcher/ibazel/command",
    visibility = ["//visibility:public"],
    deps = ["//bazel:go_default_library"],
)

go_test(
    name = "go_default_test",
    srcs = ["command_test.go"],
    importpath = "github.com/bazelbuild/bazel-watcher/ibazel/command",
    library = ":go_default_library",
)
