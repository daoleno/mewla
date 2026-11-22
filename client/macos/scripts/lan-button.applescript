on run argv
  set {windowName, buttonID} to argv
  tell application "System Events" to tell application process "Mewla"
    set controls to entire contents of window windowName
    repeat with controlRef in controls
        set identifier to missing value
        try
            set identifier to value of attribute "AXIdentifier" of controlRef
        end try
        if identifier is buttonID then
            if role of controlRef is not "AXButton" then error "Expected a button"
            if not (enabled of controlRef) then error "Button is disabled"
            perform action "AXPress" of controlRef
            return "AXPress delivered to " & buttonID
        end if
    end repeat
    error "Button is missing"
  end tell
end run
