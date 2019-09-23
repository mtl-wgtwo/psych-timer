load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_embed_data", "go_library")
load("@bazel_gazelle//:def.bzl", "gazelle")
load("@rules_pkg//:pkg.bzl", "pkg_tar", "pkg_zip")

# gazelle:prefix github.com/robothor/psych-timer
gazelle(name = "gazelle")

go_library(
    name = "go_default_library",
    srcs = [
        "main.go",
        "mindware_emitter.go",
        "psych_timer.go",
        ":static",  # keep
    ],
    importpath = "github.com/robothor/psych-timer",
    visibility = ["//visibility:private"],
    deps = [
        "@com_github_docopt_docopt_go//:go_default_library",
        "@com_github_faiface_beep//:go_default_library",
        "@com_github_faiface_beep//speaker:go_default_library",
        "@com_github_faiface_beep//wav:go_default_library",
        "@com_github_gorilla_websocket//:go_default_library",
        "@com_github_mitchellh_copystructure//:go_default_library",
        "@com_github_mitchellh_go_homedir//:go_default_library",
        "@com_github_sirupsen_logrus//:go_default_library",
        "@com_github_skratchdot_open_golang//open:go_default_library",
        "@com_github_spf13_viper//:go_default_library",
    ],
)

go_binary(
    name = "psych-timer",
    data = glob(["*.wav"]) + glob(["*.yaml"]),
    embed = [
        ":go_default_library",
    ],
    visibility = ["//visibility:public"],
)

go_embed_data(
    name = "static",
    srcs = glob(["static/**"]),
    package = "main",
    string = True,
)

# It would be better to have a directory tree, but the zip
# archiver doesn't support that.  Since Windows is kinda the
# primary target we are stuck (for now).
pkg_tar(
    name = "psych-timer-tgz",
    srcs = [
        ":psych-timer",
    ] + glob(["*.wav"]) + glob(["*.yaml"]),
    extension = "tgz",
)

pkg_zip(
    name = "psych-timer-zip",
    srcs = [
        ":psych-timer",
    ] + glob(["*.yaml"]) + glob(["*.wav"]),
    extension = "zip",
)
