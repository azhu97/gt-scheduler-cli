import sys

from gtclass.daemon import LAUNCHD_LABEL, _launchd_plist_dict


def test_launchd_plist_dict_defaults():
    plist = _launchd_plist_dict()
    assert plist["Label"] == LAUNCHD_LABEL
    assert plist["ProgramArguments"] == [
        sys.executable,
        "-m",
        "gtclass",
        "daemon",
        "start",
        "--foreground",
    ]
    assert plist["RunAtLoad"] is True
    assert plist["KeepAlive"] == {"SuccessfulExit": False}
    assert plist["StandardOutPath"] == plist["StandardErrorPath"]


def test_launchd_plist_dict_with_interval():
    plist = _launchd_plist_dict(interval=45)
    assert plist["ProgramArguments"] == [
        sys.executable,
        "-m",
        "gtclass",
        "daemon",
        "start",
        "--foreground",
        "--interval",
        "45",
    ]
