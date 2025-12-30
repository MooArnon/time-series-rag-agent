##########
# Import #
##############################################################################

import os
import requests

###########
# Classes #
##############################################################################

class DiscordNotify:
    def __init__(self, webhook_url: str) -> None:
        super().__init__()
        self.webhook_url = webhook_url
    
    ##########################################################################
    
    def sent_message(self, message: str, username: str = None) -> None:
        """To sent message

        Parameters
        ----------
        message : str
            String of message
        """
        data = {"content": message}
        if username:
            data["username"] = username

        response = requests.post(self.webhook_url, json=data)

        if response.status_code == 204:
            print("Message sent successfully!")
        else:
            print(f"""Failed to send message. 
            code: {response.status_code}, Response: {response.text}
            """
            )
    
    ##########################################################################
    
    def sent_message_image(
            self, 
            message: str, 
            file_path:os.PathLike,
            username: str = None,
        ) -> None:
        data = {"content": message}
        if username:
            data["username"] = username

        with open(file_path, 'rb') as file:
            files = {
                "file": file
            }
            response = requests.post(self.webhook_url, data=data, files=files)

        if response.status_code == 204 or response.status_code == 200:
            print("Message with file sent successfully!")
            
            # Print the attachment URL from the response
            json_response = response.json()
            attachment_url = json_response["attachments"][0]["url"]
            print(f"Image uploaded successfully: {attachment_url}")
        else:
            print(f"""Failed to send message. 
            code: {response.status_code}, Response: {response.text}
            """
            )
    
    ##########################################################################
    
##############################################################################
